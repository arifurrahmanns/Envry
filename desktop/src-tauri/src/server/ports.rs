//! Checks the ports before Envry starts, so a conflict can be explained
//! in full: every busy port, which program holds it, and whether that
//! program is another mail catcher.

use std::io::{self, Read, Write};
use std::net::{Ipv4Addr, Ipv6Addr, SocketAddr, TcpStream};
use std::time::Duration;

use serde::Serialize;
use socket2::{Domain, Protocol, Socket, Type};

use crate::settings::Ports;

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Busy {
    pub service: String,
    pub port: u16,
    /// "inUse" when another program listens on it, "blocked" when the
    /// system refuses it (on Windows usually a reserved port range).
    pub reason: &'static str,
    pub holder: Option<Holder>,
    /// "Envry" or "Mailpit" when the HTTP port answers like one of them.
    pub app: Option<&'static str>,
}

#[derive(Debug, Clone, Serialize)]
pub struct Holder {
    pub pid: u32,
    pub name: Option<String>,
}

/// Returns the ports Envry cannot open, with what is known about each.
pub fn check(ports: &Ports) -> Vec<Busy> {
    ports
        .services()
        .into_iter()
        .filter_map(|(service, port)| {
            let reason = bind_error(port)?;
            Some(describe(service, port, reason))
        })
        .collect()
}

/// Builds the description of a busy port.
pub fn describe(service: &str, port: u16, reason: &'static str) -> Busy {
    let app = if service == "http" {
        identify_http(port)
    } else {
        None
    };
    Busy {
        service: service.to_string(),
        port,
        reason,
        holder: holder(port),
        app,
    }
}

/// Tries the addresses Envry's listeners cover. Envry (Go) listens on [::]
/// in dual-stack mode, and on Windows only a dual-stack probe collides with
/// such a listener (an IPv4 or IPv6-only bind next to it succeeds), so that
/// comes first. IPv4 covers systems without IPv6; loopback catches programs
/// that listen on 127.0.0.1 only.
fn bind_error(port: u16) -> Option<&'static str> {
    let probes = [
        (SocketAddr::from((Ipv6Addr::UNSPECIFIED, port)), true),
        (SocketAddr::from((Ipv4Addr::UNSPECIFIED, port)), false),
        (SocketAddr::from((Ipv4Addr::LOCALHOST, port)), false),
    ];
    for (addr, dual_stack) in probes {
        match try_listen(addr, dual_stack) {
            Err(e) if e.kind() == io::ErrorKind::AddrInUse => return Some("inUse"),
            Err(e) if e.kind() == io::ErrorKind::PermissionDenied => return Some("blocked"),
            // Free, or this address family is unavailable (no IPv6, say).
            _ => {}
        }
    }
    None
}

/// Opens and closes a listener with the options Go uses, so the answer
/// matches what Envry will get: SO_REUSEADDR on Unix (a port held only by
/// connections in TIME_WAIT, as after a restart, is free there) and none on
/// Windows (where it would let a bind take over a port in use).
fn try_listen(addr: SocketAddr, dual_stack: bool) -> io::Result<()> {
    let s = Socket::new(Domain::for_address(addr), Type::STREAM, Some(Protocol::TCP))?;
    #[cfg(unix)]
    s.set_reuse_address(true)?;
    if dual_stack {
        s.set_only_v6(false)?;
    }
    s.bind(&addr.into())?;
    s.listen(1)
}

/// Asks the HTTP port what it is. Envry and Mailpit both answer
/// /api/v1/info, with different shapes.
fn identify_http(port: u16) -> Option<&'static str> {
    let body = http_get(port, "/api/v1/info").ok()?;
    let v: serde_json::Value = serde_json::from_str(&body).ok()?;
    let has = |k: &str| v.get(k).is_some();
    if has("ports") && has("stats") && has("settings") {
        Some("Envry")
    } else if has("Database") || has("DatabaseSize") {
        Some("Mailpit")
    } else {
        None
    }
}

fn http_get(port: u16, path: &str) -> io::Result<String> {
    let addr = SocketAddr::from((Ipv4Addr::LOCALHOST, port));
    let mut s = TcpStream::connect_timeout(&addr, Duration::from_millis(500))?;
    s.set_read_timeout(Some(Duration::from_secs(1)))?;
    s.set_write_timeout(Some(Duration::from_secs(1)))?;
    write!(
        s,
        "GET {path} HTTP/1.0\r\nHost: 127.0.0.1:{port}\r\nAccept: application/json\r\n\r\n"
    )?;
    let mut buf = Vec::new();
    s.take(256 * 1024).read_to_end(&mut buf)?;
    let text = String::from_utf8_lossy(&buf);
    let body = text
        .split_once("\r\n\r\n")
        .map(|(_, b)| b)
        .unwrap_or_default();
    Ok(body.to_string())
}

/// The process listening on port, when the system tells us.
#[cfg(windows)]
pub fn holder(port: u16) -> Option<Holder> {
    let pid = win::listener_pid(port)?;
    Some(Holder {
        pid,
        name: win::process_name(pid),
    })
}

#[cfg(not(windows))]
pub fn holder(port: u16) -> Option<Holder> {
    // lsof ships with macOS and most Linux desktops; without it the
    // message just leaves out the program name.
    let out = std::process::Command::new("lsof")
        .args(["-nP", &format!("-iTCP:{port}"), "-sTCP:LISTEN", "-Fpc"])
        .output()
        .ok()?;
    let text = String::from_utf8_lossy(&out.stdout);
    let mut pid = None;
    let mut name = None;
    for line in text.lines() {
        if let Some(p) = line.strip_prefix('p') {
            if pid.is_some() {
                break;
            }
            pid = p.parse().ok();
        } else if let Some(c) = line.strip_prefix('c') {
            name = Some(c.to_string());
        }
    }
    Some(Holder { pid: pid?, name })
}

#[cfg(windows)]
mod win {
    use std::ffi::c_void;
    use std::path::Path;

    use windows_sys::Win32::Foundation::{CloseHandle, ERROR_INSUFFICIENT_BUFFER, NO_ERROR};
    use windows_sys::Win32::NetworkManagement::IpHelper::{
        GetExtendedTcpTable, MIB_TCP6ROW_OWNER_PID, MIB_TCPROW_OWNER_PID,
        TCP_TABLE_OWNER_PID_LISTENER,
    };
    use windows_sys::Win32::Networking::WinSock::{AF_INET, AF_INET6};
    use windows_sys::Win32::System::Threading::{
        OpenProcess, QueryFullProcessImageNameW, PROCESS_NAME_WIN32,
        PROCESS_QUERY_LIMITED_INFORMATION,
    };

    /// Reads the listening-socket table for one address family.
    fn table(family: u16) -> Option<Vec<u8>> {
        let mut size = 0u32;
        // SAFETY: a null buffer with size 0 only asks for the needed size.
        let rc = unsafe {
            GetExtendedTcpTable(
                std::ptr::null_mut(),
                &mut size,
                0,
                family as u32,
                TCP_TABLE_OWNER_PID_LISTENER,
                0,
            )
        };
        if rc != ERROR_INSUFFICIENT_BUFFER {
            return None;
        }
        // Room for sockets opened between the two calls.
        size += 4096;
        let mut buf = vec![0u8; size as usize];
        // SAFETY: buf has `size` writable bytes.
        let rc = unsafe {
            GetExtendedTcpTable(
                buf.as_mut_ptr() as *mut c_void,
                &mut size,
                0,
                family as u32,
                TCP_TABLE_OWNER_PID_LISTENER,
                0,
            )
        };
        (rc == NO_ERROR).then_some(buf)
    }

    /// Rows of a MIB_TCP(6)TABLE_OWNER_PID: a u32 count, then the rows.
    fn rows<T: Copy>(buf: &[u8]) -> Vec<T> {
        let n = u32::from_ne_bytes(buf[..4].try_into().unwrap()) as usize;
        let align = std::mem::align_of::<T>().max(4);
        let start = 4usize.next_multiple_of(align);
        let size = std::mem::size_of::<T>();
        (0..n)
            .filter_map(|i| {
                let off = start + i * size;
                let bytes = buf.get(off..off + size)?;
                // SAFETY: T is a plain C struct and bytes holds size_of::<T>() bytes.
                Some(unsafe { std::ptr::read_unaligned(bytes.as_ptr() as *const T) })
            })
            .collect()
    }

    fn port_of(dw: u32) -> u16 {
        u16::from_be((dw & 0xffff) as u16)
    }

    pub fn listener_pid(port: u16) -> Option<u32> {
        if let Some(buf) = table(AF_INET) {
            if let Some(r) = rows::<MIB_TCPROW_OWNER_PID>(&buf)
                .iter()
                .find(|r| port_of(r.dwLocalPort) == port)
            {
                return Some(r.dwOwningPid);
            }
        }
        let buf = table(AF_INET6)?;
        rows::<MIB_TCP6ROW_OWNER_PID>(&buf)
            .iter()
            .find(|r| port_of(r.dwLocalPort) == port)
            .map(|r| r.dwOwningPid)
    }

    pub fn process_name(pid: u32) -> Option<String> {
        // SAFETY: plain Win32 calls; the handle is closed before returning.
        unsafe {
            let h = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
            if h.is_null() {
                return None;
            }
            let mut buf = [0u16; 1024];
            let mut len = buf.len() as u32;
            let ok = QueryFullProcessImageNameW(h, PROCESS_NAME_WIN32, buf.as_mut_ptr(), &mut len);
            CloseHandle(h);
            if ok == 0 {
                return None;
            }
            let full = String::from_utf16_lossy(&buf[..len as usize]);
            Path::new(&full)
                .file_name()
                .map(|n| n.to_string_lossy().into_owned())
        }
    }
}

#[cfg(test)]
mod tests {
    use std::net::TcpListener;

    use super::*;

    #[test]
    fn finds_a_busy_port_and_its_holder() {
        let l = TcpListener::bind("0.0.0.0:0").unwrap();
        let port = l.local_addr().unwrap().port();
        assert_eq!(bind_error(port), Some("inUse"));
        let busy = describe("smtp", port, "inUse");
        // Elsewhere the holder comes from lsof, which may not be installed.
        if cfg!(windows) || busy.holder.is_some() {
            let h = busy.holder.expect("holder of our own socket");
            assert_eq!(h.pid, std::process::id());
        }
    }

    #[test]
    fn finds_a_dual_stack_listener() {
        // How Envry itself listens: [::] with IPv4 mapped in.
        let Ok(s) = Socket::new(Domain::IPV6, Type::STREAM, Some(Protocol::TCP)) else {
            return;
        };
        if s.set_only_v6(false).is_err()
            || s.bind(&SocketAddr::from((Ipv6Addr::UNSPECIFIED, 0)).into())
                .is_err()
        {
            return; // no IPv6 here
        }
        s.listen(1).unwrap();
        let port = s.local_addr().unwrap().as_socket().unwrap().port();
        assert_eq!(bind_error(port), Some("inUse"));
    }

    /// Like a port Envry served mail on just before a restart: the server
    /// closed first, so its end of the connection waits in TIME_WAIT. Go
    /// can listen there again on Unix, so it must not count as busy.
    #[cfg(unix)]
    #[test]
    fn time_wait_is_not_busy() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let client = TcpStream::connect(("127.0.0.1", port)).unwrap();
        let (server, _) = listener.accept().unwrap();
        drop(server);
        std::thread::sleep(Duration::from_millis(50));
        drop(client);
        drop(listener);
        assert_eq!(bind_error(port), None);
    }

    #[test]
    fn free_port_is_not_reported() {
        let port = TcpListener::bind("127.0.0.1:0")
            .unwrap()
            .local_addr()
            .unwrap()
            .port();
        assert_eq!(bind_error(port), None);
    }
}
