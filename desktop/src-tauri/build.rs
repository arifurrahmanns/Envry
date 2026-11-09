fn main() {
    // Declaring the app's commands gives each an allow-<command> permission,
    // so the capabilities decide which page may call which command: the
    // bundled status page its four, Envry's web UI only set_tls.
    tauri_build::try_build(tauri_build::Attributes::new().app_manifest(
        tauri_build::AppManifest::new().commands(&[
            "get_status",
            "start_server",
            "save_ports",
            "open_log",
            "set_tls",
            "certificate_status",
            "trust_certificate",
        ]),
    ))
    .expect("failed to run tauri-build");
}
