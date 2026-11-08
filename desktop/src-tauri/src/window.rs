//! The main window. It shows the bundled status page while Envry is not
//! running, and Envry's own web UI (from 127.0.0.1) while it is.

use tauri::webview::NewWindowResponse;
use tauri::window::Color;
use tauri::{AppHandle, Manager, Url, WebviewUrl, WebviewWindow, WebviewWindowBuilder};
use tauri_plugin_opener::OpenerExt;

use crate::server::{Phase, Server};

pub const LABEL: &str = "main";

/// Keeps the pages feeling like an app rather than a browser on macOS and
/// Linux (Windows uses WebView2 settings, see `lock_down`): no right-click
/// menu, and in release builds no reload, find, print, view-source or
/// DevTools shortcuts. App shortcuts (Ctrl+K, j, k, /, r, u, #) still work.
const LOCK_DOWN_JS: &str = r#"(() => {
  addEventListener('contextmenu', (e) => e.preventDefault(), true)
  if (!__BLOCK_KEYS__) return
  addEventListener('keydown', (e) => {
    const k = e.key.toLowerCase()
    const mod = e.ctrlKey || e.metaKey
    if (k === 'f5' || k === 'f7' || k === 'f12'
      || (mod && ['r', 'p', 'f', 'g', 'u', 's', '=', '+', '-', '0'].includes(k))
      || (mod && e.shiftKey && ['i', 'j', 'c'].includes(k))
      || (e.altKey && (k === 'arrowleft' || k === 'arrowright'))) {
      e.preventDefault()
    }
  }, true)
})()"#;

fn main(app: &AppHandle) -> Option<WebviewWindow> {
    app.get_webview_window(LABEL)
}

pub fn create(app: &AppHandle, visible: bool) -> tauri::Result<()> {
    let nav = app.clone();
    let popup = app.clone();
    let lock_down_js = LOCK_DOWN_JS.replace(
        "__BLOCK_KEYS__",
        if cfg!(debug_assertions) {
            "false"
        } else {
            "true"
        },
    );
    let builder = WebviewWindowBuilder::new(app, LABEL, WebviewUrl::App("index.html".into()))
        .initialization_script(lock_down_js)
        .title("Envry")
        .inner_size(1280.0, 820.0)
        // Fits the sidebar, list and reader at their minimum widths (web/src: AppShell, MailPanes).
        .min_inner_size(1024.0, 640.0)
        .visible(visible)
        .background_color(Color(10, 10, 11, 255));
    // No native title bar: the pages fill the window and draw their own
    // minimize, maximize and close buttons in the top-right corner
    // (web/src/components/layout/window-controls.tsx and src/index.html).
    // macOS keeps its native traffic lights over the top-left corner.
    #[cfg(target_os = "macos")]
    let builder = builder
        .title_bar_style(tauri::TitleBarStyle::Overlay)
        .hidden_title(true);
    #[cfg(not(target_os = "macos"))]
    let builder = builder.decorations(false);
    let window = builder
        .on_navigation(move |url| {
            if is_status_page(&nav, url)
                || is_ui(&nav, url)
                || matches!(url.scheme(), "about" | "data" | "blob")
            {
                return true;
            }
            open_external(&nav, url);
            false
        })
        .on_new_window(move |url, _| {
            open_external(&popup, &url);
            NewWindowResponse::Deny
        })
        .build()?;
    lock_down(&window);
    Ok(())
}

/// Turns off WebView2's browser behaviour: its right-click menu (including
/// inside email previews) and, in release builds, browser shortcuts such as
/// reload, find, print and DevTools. Debug builds keep F12 for development.
/// Zoom hotkeys are already off (Tauri's default).
#[cfg(windows)]
fn lock_down(window: &WebviewWindow) {
    let _ = window.with_webview(|webview| {
        use webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2Settings3;
        use windows_core::Interface;
        // SAFETY: COM calls on the live WebView2, made on the thread Tauri
        // runs this closure on.
        unsafe {
            let Ok(settings) = webview
                .controller()
                .CoreWebView2()
                .and_then(|core| core.Settings())
            else {
                return;
            };
            let _ = settings.SetAreDefaultContextMenusEnabled(false);
            if !cfg!(debug_assertions) {
                if let Ok(settings) = settings.cast::<ICoreWebView2Settings3>() {
                    let _ = settings.SetAreBrowserAcceleratorKeysEnabled(false);
                }
            }
        }
    });
}

#[cfg(not(windows))]
fn lock_down(_window: &WebviewWindow) {}

fn open_external(app: &AppHandle, url: &Url) {
    if matches!(url.scheme(), "http" | "https" | "mailto") {
        let _ = app.opener().open_url(url.as_str(), None::<&str>);
    }
}

/// The bundled status page (src/index.html). Tauri serves it from
/// tauri://localhost, or http://tauri.localhost on Windows; under
/// `tauri dev` the CLI's dev server serves it instead.
fn status_url(app: &AppHandle) -> Url {
    let base = match &app.config().build.dev_url {
        Some(dev) if tauri::is_dev() => dev.clone(),
        _ => {
            let base = if cfg!(windows) {
                "http://tauri.localhost/"
            } else {
                "tauri://localhost/"
            };
            Url::parse(base).expect("static URL")
        }
    };
    base.join("index.html").expect("static URL")
}

fn is_status_page(app: &AppHandle, url: &Url) -> bool {
    let page = status_url(app);
    url.scheme() == page.scheme()
        && url.host_str() == page.host_str()
        && url.port_or_known_default() == page.port_or_known_default()
}

fn ui_port(app: &AppHandle) -> Option<u16> {
    match app.state::<Server>().phase() {
        Phase::Running { ports, .. } => Some(ports.http),
        _ => None,
    }
}

fn is_ui(app: &AppHandle, url: &Url) -> bool {
    url.scheme() == "http"
        && matches!(url.host_str(), Some("127.0.0.1") | Some("localhost"))
        && url.port().is_some()
        && url.port() == ui_port(app)
}

/// Shows the web UI while Envry runs and the status page otherwise.
pub fn follow(app: &AppHandle, phase: &Phase) {
    let Some(w) = main(app) else { return };
    let current = w.url().ok();
    match phase {
        Phase::Running { ports, .. } => {
            let on_ui = current.as_ref().is_some_and(|u| is_ui(app, u));
            if !on_ui {
                if let Ok(url) = Url::parse(&format!("http://127.0.0.1:{}/", ports.http)) {
                    let _ = w.navigate(url);
                }
            }
        }
        _ => {
            if !current.as_ref().is_some_and(|u| is_status_page(app, u)) {
                let _ = w.navigate(status_url(app));
            }
        }
    }
}

pub fn show(app: &AppHandle) {
    if let Some(w) = main(app) {
        let _ = w.unminimize();
        let _ = w.show();
        let _ = w.set_focus();
    }
}

/// Shows the window, at the given message when Envry is running.
pub fn open_message(app: &AppHandle, message: Option<String>) {
    show(app);
    let (Some(port), Some(w)) = (ui_port(app), main(app)) else {
        return;
    };
    let path = match message {
        Some(id) => format!("message/{id}"),
        None => "sent".to_string(),
    };
    if let Ok(url) = Url::parse(&format!("http://127.0.0.1:{port}/{path}")) {
        let _ = w.navigate(url);
    }
}

/// Whether the user is looking at the window right now.
pub fn is_looking(app: &AppHandle) -> bool {
    main(app).is_some_and(|w| {
        w.is_visible().unwrap_or(false)
            && w.is_focused().unwrap_or(false)
            && !w.is_minimized().unwrap_or(false)
    })
}

/// Badges the taskbar or dock button with the new-mail count.
pub fn set_badge(app: &AppHandle, new: u32) {
    let Some(w) = main(app) else { return };
    #[cfg(windows)]
    {
        let dot = (new > 0).then(|| {
            tauri::image::Image::from_bytes(include_bytes!("../icons/tray/badge.png"))
                .expect("bundled overlay icon")
        });
        let _ = w.set_overlay_icon(dot);
    }
    #[cfg(not(windows))]
    {
        let _ = w.set_badge_count((new > 0).then_some(i64::from(new)));
    }
}
