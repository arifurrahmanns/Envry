//! Envry's self-signed certificate and this computer's trust in it. Apps
//! that check certificates (most mail libraries do) refuse Envry's SSL/TLS
//! ports until the certificate is trusted, so the Connection page shows
//! whether it is and offers to add it to the user's trusted certificates.
//! The system asks the user to confirm before it does.

use std::path::{Path, PathBuf};

use serde::Serialize;
use tauri::{AppHandle, Manager};

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Status {
    /// Where Envry keeps its certificate.
    path: String,
    /// Whether this computer trusts it; null where Envry cannot tell.
    trusted: Option<bool>,
    /// Whether Envry can add it to the trusted certificates here.
    can_trust: bool,
}

/// Envry keeps its certificate next to its database (`tlsDir` in
/// internal/app): ~/.envry/tls unless ENVRY_DB points elsewhere.
fn cert_path(app: &AppHandle) -> Result<PathBuf, String> {
    let home = app.path().home_dir().map_err(|e| e.to_string())?;
    let dir = match std::env::var("ENVRY_DB") {
        Ok(db) if !db.is_empty() && db != ":memory:" => {
            let db = match db.strip_prefix("~/").or_else(|| db.strip_prefix("~\\")) {
                Some(rest) => home.join(rest),
                None => PathBuf::from(db),
            };
            db.parent().map(Path::to_path_buf).unwrap_or_default()
        }
        _ => home.join(".envry"),
    };
    Ok(dir.join("tls").join("cert.pem"))
}

#[tauri::command]
pub fn certificate_status(app: AppHandle) -> Result<Status, String> {
    let path = cert_path(&app)?;
    Ok(Status {
        trusted: os::is_trusted(&path)?,
        path: path.display().to_string(),
        can_trust: os::CAN_TRUST,
    })
}

/// Adds Envry's certificate to the user's trusted certificates. Waits for
/// the user to answer the system's confirmation, so it runs off the main
/// thread.
#[tauri::command]
pub async fn trust_certificate(app: AppHandle) -> Result<(), String> {
    let path = cert_path(&app)?;
    tauri::async_runtime::spawn_blocking(move || os::trust(&path))
        .await
        .map_err(|e| e.to_string())?
}

/// The Windows certificate store of the current user ("Trusted Root
/// Certification Authorities"), which SChannel and so native-tls, .NET,
/// Go and most Windows mail clients consult.
#[cfg(windows)]
mod os {
    use std::ffi::c_void;
    use std::path::Path;
    use std::ptr::{null, null_mut};

    use windows_sys::Win32::Foundation::{GetLastError, ERROR_CANCELLED};
    use windows_sys::Win32::Security::Cryptography::{
        CertAddCertificateContextToStore, CertCloseStore, CertCreateCertificateContext,
        CertFindCertificateInStore, CertFreeCertificateContext, CertOpenSystemStoreW,
        CryptStringToBinaryA, CERT_CONTEXT, CERT_FIND_EXISTING, CERT_STORE_ADD_USE_EXISTING,
        CRYPT_STRING_BASE64HEADER, HCERTSTORE, X509_ASN_ENCODING,
    };

    pub const CAN_TRUST: bool = true;

    fn read(path: &Path) -> Result<Vec<u8>, String> {
        std::fs::read(path).map_err(|e| format!("Couldn't read {}: {e}", path.display()))
    }

    struct Store(HCERTSTORE);

    impl Store {
        /// The user's trusted roots, which include the machine's.
        fn root() -> Result<Self, String> {
            let name: Vec<u16> = "ROOT\0".encode_utf16().collect();
            // SAFETY: name is a NUL-terminated UTF-16 string.
            let h = unsafe { CertOpenSystemStoreW(0, name.as_ptr()) };
            if h.is_null() {
                return Err("Couldn't open the Windows certificate store.".into());
            }
            Ok(Store(h))
        }
    }

    impl Drop for Store {
        fn drop(&mut self) {
            // SAFETY: the handle came from CertOpenSystemStoreW.
            unsafe { CertCloseStore(self.0, 0) };
        }
    }

    struct Cert(*mut CERT_CONTEXT);

    impl Cert {
        /// Parses a PEM certificate file's contents.
        fn from_pem(pem: &[u8]) -> Result<Self, String> {
            let invalid = || "Envry's certificate file is not valid.".to_string();
            let decode = |out: *mut u8, len: &mut u32| {
                // SAFETY: pem is valid for its length; out is null (to
                // measure) or has room for len bytes.
                unsafe {
                    CryptStringToBinaryA(
                        pem.as_ptr(),
                        pem.len() as u32,
                        CRYPT_STRING_BASE64HEADER,
                        out,
                        len,
                        null_mut(),
                        null_mut(),
                    )
                }
            };
            let mut len = 0u32;
            if decode(null_mut(), &mut len) == 0 {
                return Err(invalid());
            }
            let mut der = vec![0u8; len as usize];
            if decode(der.as_mut_ptr(), &mut len) == 0 {
                return Err(invalid());
            }
            // SAFETY: der holds len bytes; Windows copies them.
            let ctx = unsafe { CertCreateCertificateContext(X509_ASN_ENCODING, der.as_ptr(), len) };
            if ctx.is_null() {
                return Err(invalid());
            }
            Ok(Cert(ctx))
        }
    }

    impl Drop for Cert {
        fn drop(&mut self) {
            // SAFETY: the context came from CertCreateCertificateContext.
            unsafe { CertFreeCertificateContext(self.0) };
        }
    }

    pub fn is_trusted(path: &Path) -> Result<Option<bool>, String> {
        let cert = Cert::from_pem(&read(path)?)?;
        let store = Store::root()?;
        // SAFETY: both handles are live; a found context is freed at once.
        let found = unsafe {
            CertFindCertificateInStore(
                store.0,
                X509_ASN_ENCODING,
                0,
                CERT_FIND_EXISTING,
                cert.0 as *const c_void,
                null(),
            )
        };
        if found.is_null() {
            return Ok(Some(false));
        }
        // SAFETY: found is a context the store returned.
        unsafe { CertFreeCertificateContext(found) };
        Ok(Some(true))
    }

    pub fn trust(path: &Path) -> Result<(), String> {
        let cert = Cert::from_pem(&read(path)?)?;
        let store = Store::root()?;
        // Windows shows its security warning and adds the certificate only
        // if the user confirms.
        // SAFETY: both handles are live.
        let ok = unsafe {
            CertAddCertificateContextToStore(
                store.0,
                cert.0,
                CERT_STORE_ADD_USE_EXISTING,
                null_mut(),
            )
        };
        if ok == 0 {
            // SAFETY: reads this thread's last error.
            let code = unsafe { GetLastError() };
            return Err(if code == ERROR_CANCELLED {
                "The certificate was not added.".to_string()
            } else {
                format!("Windows didn't add the certificate (error {code}).")
            });
        }
        Ok(())
    }
}

/// The login keychain, whose trust settings Apple's TLS and Mail use.
#[cfg(target_os = "macos")]
mod os {
    use std::path::Path;
    use std::process::Command;

    pub const CAN_TRUST: bool = true;

    pub fn is_trusted(path: &Path) -> Result<Option<bool>, String> {
        // Succeeds only when the certificate is trusted for TLS servers.
        let status = Command::new("/usr/bin/security")
            .args([
                "verify-cert",
                "-q",
                "-L",
                "-p",
                "ssl",
                "-s",
                "localhost",
                "-c",
            ])
            .arg(path)
            .status()
            .map_err(|e| e.to_string())?;
        Ok(Some(status.success()))
    }

    pub fn trust(path: &Path) -> Result<(), String> {
        let home = std::env::var("HOME").map_err(|e| e.to_string())?;
        let keychain = Path::new(&home).join("Library/Keychains/login.keychain-db");
        // macOS asks for the user's password to change trust settings.
        let out = Command::new("/usr/bin/security")
            .args(["add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k"])
            .arg(keychain)
            .arg(path)
            .output()
            .map_err(|e| e.to_string())?;
        if out.status.success() {
            Ok(())
        } else {
            Err(String::from_utf8_lossy(&out.stderr).trim().to_string())
        }
    }
}

/// Linux has no per-user trust store that every program reads, so the
/// Connection page shows the certificate file to add by hand.
#[cfg(not(any(windows, target_os = "macos")))]
mod os {
    use std::path::Path;

    pub const CAN_TRUST: bool = false;

    pub fn is_trusted(_path: &Path) -> Result<Option<bool>, String> {
        Ok(None)
    }

    pub fn trust(_path: &Path) -> Result<(), String> {
        Err("Add the certificate to your system's trusted certificates by hand.".into())
    }
}
