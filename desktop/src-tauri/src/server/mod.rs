//! Runs the bundled `envry` binary and tracks its state.
//!
//! Envry is started with `--managed`: it prints a JSON "ready" or "error"
//! line on stdout and shuts down cleanly when its stdin closes. Stopping
//! therefore closes stdin, and if this app dies the pipe closes on its own,
//! so the server never outlives the app.

mod ports;

use std::collections::VecDeque;
use std::fs::{File, OpenOptions};
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::{Mutex, MutexGuard};
use std::thread;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager};

use self::ports::Busy;
use crate::settings::{self, Ports};

const STOP_TIMEOUT: Duration = Duration::from_secs(5);
const LOG_TAIL: usize = 30;

#[derive(Debug, Clone, Default, Serialize)]
#[serde(tag = "phase", rename_all = "camelCase")]
pub enum Phase {
    #[default]
    Stopped,
    Starting,
    #[serde(rename_all = "camelCase")]
    Running {
        version: String,
        ports: Ports,
    },
    Conflict {
        busy: Vec<Busy>,
    },
    Failed {
        message: String,
        hint: Option<String>,
        log: Vec<String>,
    },
}

/// One line of `envry serve --managed` output.
#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct StatusLine {
    event: String,
    #[serde(default)]
    version: String,
    addrs: Option<Addrs>,
    #[serde(default)]
    message: String,
    service: Option<String>,
    addr: Option<String>,
    #[serde(default)]
    in_use: bool,
    hint: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
struct Addrs {
    http: String,
    smtp: String,
    pop3: String,
    imap: String,
    /// The SSL/TLS ports, sent only when secure connections are on.
    smtps: Option<String>,
    pop3s: Option<String>,
    imaps: Option<String>,
}

#[derive(Default)]
struct Inner {
    /// Bumped on every start and stop; threads of an older run give up.
    gen: u64,
    child: Option<Child>,
    stdin: Option<ChildStdin>,
    stopping: bool,
    /// Set by `restart` until the new run is up or has failed, so the
    /// window keeps its page instead of showing the status page meanwhile.
    quiet: bool,
    last_error: Option<StatusLine>,
    phase: Phase,
}

#[derive(Default)]
pub struct Server {
    inner: Mutex<Inner>,
    tail: Mutex<VecDeque<String>>,
    log_path: Mutex<Option<PathBuf>>,
}

fn lock<T>(m: &Mutex<T>) -> MutexGuard<'_, T> {
    m.lock().unwrap_or_else(|e| e.into_inner())
}

impl Server {
    pub fn set_log_path(&self, p: PathBuf) {
        *lock(&self.log_path) = Some(p);
    }

    pub fn log_path(&self) -> Option<PathBuf> {
        lock(&self.log_path).clone()
    }

    /// The current run and its phase.
    pub fn current(&self) -> (u64, Phase) {
        let i = lock(&self.inner);
        (i.gen, i.phase.clone())
    }

    pub fn phase(&self) -> Phase {
        lock(&self.inner).phase.clone()
    }

    /// Whether Envry is restarting on request (see `restart`).
    pub fn restarting(&self) -> bool {
        lock(&self.inner).quiet
    }

    fn set_phase(&self, app: &AppHandle, gen: u64, phase: Phase) {
        {
            let mut i = lock(&self.inner);
            if i.gen != gen {
                return;
            }
            if !matches!(phase, Phase::Stopped | Phase::Starting) {
                i.quiet = false;
            }
            i.phase = phase;
        }
        crate::phase_changed(app);
    }

    /// Checks the ports and starts Envry in the background with the current
    /// settings. Does nothing while a server is starting or running.
    pub fn start(&self, app: &AppHandle) {
        let gen = {
            let mut i = lock(&self.inner);
            if i.child.is_some() || matches!(i.phase, Phase::Starting) {
                return;
            }
            i.gen += 1;
            i.phase = Phase::Starting;
            i.stopping = false;
            i.last_error = None;
            i.gen
        };
        crate::phase_changed(app);
        let app = app.clone();
        thread::spawn(move || app.state::<Server>().launch(&app, gen));
    }

    fn launch(&self, app: &AppHandle, gen: u64) {
        let settings = settings::get(app);
        let (ports, tls) = (settings.ports, settings.tls);
        let busy = ports::check(&ports, tls);
        if !busy.is_empty() {
            self.set_phase(app, gen, Phase::Conflict { busy });
            return;
        }

        let exe = sidecar_path();
        let mut cmd = Command::new(&exe);
        cmd.arg("serve")
            .arg("--managed")
            .args(["--smtp", &format!("0.0.0.0:{}", ports.smtp)])
            .args(["--pop3", &format!("0.0.0.0:{}", ports.pop3)])
            .args(["--imap", &format!("0.0.0.0:{}", ports.imap)])
            .args(["--http", &format!("0.0.0.0:{}", ports.http)])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        if tls {
            // STARTTLS on the ports above, plus the SSL/TLS ports. Envry makes
            // and keeps a self-signed certificate next to its database.
            cmd.arg("--tls")
                .args(["--smtps", &format!("0.0.0.0:{}", ports.smtps)])
                .args(["--pop3s", &format!("0.0.0.0:{}", ports.pop3s)])
                .args(["--imaps", &format!("0.0.0.0:{}", ports.imaps)]);
        }
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            const CREATE_NO_WINDOW: u32 = 0x0800_0000;
            cmd.creation_flags(CREATE_NO_WINDOW);
        }

        let mut child = match cmd.spawn() {
            Ok(c) => c,
            Err(e) => {
                let message = format!("Could not run {}: {e}", exe.display());
                self.set_phase(
                    app,
                    gen,
                    Phase::Failed {
                        message,
                        hint: None,
                        log: vec![],
                    },
                );
                return;
            }
        };
        let stdout = child.stdout.take();
        let stderr = child.stderr.take();
        {
            let mut i = lock(&self.inner);
            if i.gen != gen {
                // Stopped while the ports were checked.
                let _ = child.kill();
                return;
            }
            i.stdin = child.stdin.take();
            i.child = Some(child);
        }

        lock(&self.tail).clear();
        let log = self.open_log(&ports);
        if let Some(err) = stderr {
            thread::spawn({
                let app = app.clone();
                move || {
                    let s = app.state::<Server>();
                    let mut log = log;
                    for line in BufReader::new(err).lines().map_while(Result::ok) {
                        if let Some(f) = log.as_mut() {
                            let _ = writeln!(f, "{line}");
                        }
                        let mut t = lock(&s.tail);
                        if t.len() == LOG_TAIL {
                            t.pop_front();
                        }
                        t.push_back(line);
                    }
                }
            });
        }
        if let Some(out) = stdout {
            thread::spawn({
                let app = app.clone();
                move || {
                    let s = app.state::<Server>();
                    for line in BufReader::new(out).lines().map_while(Result::ok) {
                        let Ok(st) = serde_json::from_str::<StatusLine>(&line) else {
                            continue;
                        };
                        match st.event.as_str() {
                            "ready" => {
                                let p = st
                                    .addrs
                                    .as_ref()
                                    .map(|a| bound_ports(a, ports))
                                    .unwrap_or(ports);
                                s.set_phase(
                                    &app,
                                    gen,
                                    Phase::Running {
                                        version: st.version.clone(),
                                        ports: p,
                                    },
                                );
                            }
                            "error" => {
                                let mut i = lock(&s.inner);
                                if i.gen == gen {
                                    i.last_error = Some(st);
                                    // Envry exits after reporting; closing stdin
                                    // makes sure nothing keeps it waiting.
                                    drop(i.stdin.take());
                                }
                            }
                            _ => {}
                        }
                    }
                }
            });
        }
        let app = app.clone();
        thread::spawn(move || app.state::<Server>().watch_exit(&app, gen));
    }

    fn open_log(&self, ports: &Ports) -> Option<File> {
        let path = self.log_path()?;
        if let Some(dir) = path.parent() {
            let _ = std::fs::create_dir_all(dir);
        }
        let mut f = OpenOptions::new()
            .create(true)
            .append(true)
            .open(path)
            .ok()?;
        let _ = writeln!(
            f,
            "--- starting envry: smtp {} pop3 {} imap {} http {}",
            ports.smtp, ports.pop3, ports.imap, ports.http
        );
        Some(f)
    }

    /// Polls the child until it exits, then records why.
    fn watch_exit(&self, app: &AppHandle, gen: u64) {
        loop {
            thread::sleep(Duration::from_millis(200));
            let (code, stopping, last_error) = {
                let mut i = lock(&self.inner);
                if i.gen != gen {
                    return;
                }
                let Some(child) = i.child.as_mut() else {
                    return;
                };
                match child.try_wait() {
                    Ok(None) => continue,
                    Ok(Some(status)) => (status.code(), i.stopping, i.last_error.take()),
                    Err(_) => (None, i.stopping, i.last_error.take()),
                }
            };
            {
                let mut i = lock(&self.inner);
                i.child = None;
                i.stdin = None;
            }
            let phase = if stopping {
                Phase::Stopped
            } else {
                self.failure(code, last_error)
            };
            self.set_phase(app, gen, phase);
            return;
        }
    }

    /// Explains an exit nobody asked for.
    fn failure(&self, code: Option<i32>, err: Option<StatusLine>) -> Phase {
        if let Some(e) = &err {
            if e.in_use {
                let service = e.service.clone().unwrap_or_default();
                let port = e.addr.as_deref().and_then(port_of).unwrap_or(0);
                return Phase::Conflict {
                    busy: vec![ports::describe(&service, port, "inUse")],
                };
            }
        }
        let log: Vec<String> = lock(&self.tail).iter().cloned().collect();
        let message = match &err {
            Some(e) if !e.message.is_empty() => e.message.clone(),
            _ => match code {
                Some(c) => format!("Envry stopped unexpectedly (exit code {c})."),
                None => "Envry stopped unexpectedly.".to_string(),
            },
        };
        Phase::Failed {
            message,
            hint: err.and_then(|e| e.hint).filter(|h| !h.is_empty()),
            log,
        }
    }

    /// Asks Envry to stop and returns at once; a stuck server is killed
    /// after a grace period.
    pub fn stop(&self, app: &AppHandle) {
        let gen = {
            let mut i = lock(&self.inner);
            i.quiet = false;
            if i.child.is_none() {
                // Not running (maybe still checking ports): cancel that run.
                i.gen += 1;
                i.phase = Phase::Stopped;
                drop(i);
                crate::phase_changed(app);
                return;
            }
            i.stopping = true;
            drop(i.stdin.take());
            i.gen
        };
        let app = app.clone();
        thread::spawn(move || {
            thread::sleep(STOP_TIMEOUT);
            let s = app.state::<Server>();
            let mut i = lock(&s.inner);
            if i.gen == gen {
                if let Some(c) = i.child.as_mut() {
                    let _ = c.kill();
                }
            }
        });
    }

    /// Stops Envry and waits until it has exited (killing it after the
    /// grace period). Used before restarting and when the app quits.
    pub fn stop_and_wait(&self) {
        {
            let mut i = lock(&self.inner);
            i.stopping = true;
            drop(i.stdin.take());
        }
        let deadline = Instant::now() + STOP_TIMEOUT;
        loop {
            {
                let mut i = lock(&self.inner);
                let Some(child) = i.child.as_mut() else { break };
                if !matches!(child.try_wait(), Ok(None)) {
                    i.child = None;
                    break;
                }
                if Instant::now() >= deadline {
                    let _ = child.kill();
                    let _ = child.wait();
                    i.child = None;
                    break;
                }
            }
            thread::sleep(Duration::from_millis(50));
        }
        let mut i = lock(&self.inner);
        i.gen += 1;
        i.phase = Phase::Stopped;
    }

    /// Stops Envry and starts it again with the current settings. The
    /// window keeps showing its page meanwhile, unless the new run fails.
    pub fn restart(&self, app: &AppHandle) {
        lock(&self.inner).quiet = true;
        let app = app.clone();
        thread::spawn(move || {
            let s = app.state::<Server>();
            s.stop_and_wait();
            s.start(&app);
        });
    }
}

fn port_of(addr: &str) -> Option<u16> {
    addr.rsplit_once(':').and_then(|(_, p)| p.parse().ok())
}

/// The ports Envry reports it opened; `asked` fills in what it did not
/// report (the SSL/TLS ports when secure connections are off).
fn bound_ports(a: &Addrs, asked: Ports) -> Ports {
    let tls = |addr: &Option<String>, fallback: u16| {
        addr.as_deref().and_then(port_of).unwrap_or(fallback)
    };
    Ports {
        smtp: port_of(&a.smtp).unwrap_or(asked.smtp),
        pop3: port_of(&a.pop3).unwrap_or(asked.pop3),
        imap: port_of(&a.imap).unwrap_or(asked.imap),
        http: port_of(&a.http).unwrap_or(asked.http),
        smtps: tls(&a.smtps, asked.smtps),
        pop3s: tls(&a.pop3s, asked.pop3s),
        imaps: tls(&a.imaps, asked.imaps),
    }
}

/// The bundled binary sits next to this executable: Tauri installs it
/// there, and copies it there for `tauri dev`.
fn sidecar_path() -> PathBuf {
    let exe = std::env::current_exe().unwrap_or_default();
    let dir = exe.parent().map(Path::to_path_buf).unwrap_or_default();
    dir.join(if cfg!(windows) { "envry.exe" } else { "envry" })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_status_lines() {
        let ready: StatusLine = serde_json::from_str(
            r#"{"event":"ready","version":"v0.2.0","addrs":{"http":"0.0.0.0:8025","smtp":"[::]:1025","pop3":"0.0.0.0:1110","imap":"0.0.0.0:1143"}}"#,
        )
        .unwrap();
        assert_eq!(
            bound_ports(ready.addrs.as_ref().unwrap(), Ports::default()),
            Ports::default()
        );

        let err: StatusLine = serde_json::from_str(
            r#"{"event":"error","message":"listen smtp 0.0.0.0:1025: in use","service":"smtp","addr":"0.0.0.0:1025","inUse":true,"hint":"Port 1025 (SMTP) is already in use."}"#,
        )
        .unwrap();
        assert!(err.in_use);
        assert_eq!(err.addr.as_deref().and_then(port_of), Some(1025));
    }
}
