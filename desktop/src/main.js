// Status page: shown while Envry is starting, stopped, or cannot start.
// Once Envry runs, the app replaces this page with Envry's web UI.
// Uses the global Tauri API (app.withGlobalTauri in tauri.conf.json).
const { invoke } = window.__TAURI__.core
const { listen } = window.__TAURI__.event
const { getCurrentWindow } = window.__TAURI__.window

const SERVICES = [
  ['smtp', 'SMTP'],
  ['pop3', 'POP3'],
  ['imap', 'IMAP'],
  ['http', 'Web UI'],
]
// The SSL/TLS ports, opened only when secure connections are on.
const TLS_SERVICES = [
  ['smtps', 'SMTPS'],
  ['pop3s', 'POP3S'],
  ['imaps', 'IMAPS'],
]
// Same alternatives the CLI suggests (cmd/envry/hint.go).
const ALTERNATIVE = { smtp: 2525, pop3: 2110, imap: 2143, http: 8026, smtps: 2465, pop3s: 2995, imaps: 2993 }

/** The services Envry opens with these settings. */
function services(tls) {
  return tls ? [...SERVICES, ...TLS_SERVICES] : SERVICES
}

const view = document.getElementById('view')

function el(tag, attrs = {}, ...children) {
  const e = document.createElement(tag)
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') e.className = v
    else if (k.startsWith('on')) e.addEventListener(k.slice(2), v)
    else if (v !== false && v != null) e.setAttribute(k, v === true ? '' : v)
  }
  for (const c of children.flat()) if (c != null && c !== false) e.append(c)
  return e
}

function label(service) {
  return services(true).find(([s]) => s === service)?.[1] ?? service.toUpperCase()
}

function status(kind, text) {
  const dot = kind === 'spin' ? el('span', { class: 'spinner' }) : el('span', { class: `dot ${kind}` })
  return el('div', { class: 'status' }, dot, text)
}

async function call(cmd, args, button) {
  if (button) button.disabled = true
  try {
    await invoke(cmd, args)
    return null
  } catch (e) {
    return String(e)
  } finally {
    if (button) button.disabled = false
  }
}

function whoHolds(b) {
  if (b.reason === 'blocked') return 'reserved by the system'
  if (b.app === 'Envry') return 'another Envry is running here'
  const name = b.holder?.name
  if (b.app === 'Mailpit') return name ? `Mailpit (${name})` : 'Mailpit'
  if (name) return `used by ${name} (PID ${b.holder.pid})`
  if (b.holder) return `used by PID ${b.holder.pid}`
  return 'used by another program'
}

/** Port editor; "Use other ports" pre-fills alternatives for the `busy` ones. */
function portsForm(ports, tls, { open = false, busy = [] } = {}) {
  const inputs = {}
  const err = el('p', { class: 'error-text', hidden: true })
  const fields = services(tls).map(([key, name]) => {
    const input = el('input', { type: 'number', min: 1, max: 65535, value: ports[key], 'aria-label': `${name} port` })
    input.addEventListener('input', () => input.classList.toggle('changed', Number(input.value) !== ports[key]))
    inputs[key] = input
    return el('label', {}, name, input)
  })
  const save = el('button', { class: 'primary', type: 'submit' }, 'Save and start')
  const suggest = busy.length
    ? el(
        'button',
        {
          type: 'button',
          onclick: () => {
            for (const b of busy) {
              const input = inputs[b.service]
              if (input) {
                input.value = ALTERNATIVE[b.service]
                input.classList.toggle('changed', true)
              }
            }
          },
        },
        'Use other ports',
      )
    : null
  const form = el(
    'form',
    {
      onsubmit: async (e) => {
        e.preventDefault()
        // Ports not shown here (the SSL/TLS ones while they are off) stay as they are.
        const edited = Object.fromEntries(Object.entries(inputs).map(([k, input]) => [k, Number(input.value)]))
        const problem = await call('save_ports', { ports: { ...ports, ...edited } }, save)
        err.hidden = !problem
        err.textContent = problem ?? ''
      },
    },
    el('div', { class: 'ports' }, fields),
    err,
    el('div', { class: 'actions' }, save, suggest),
    el('p', { class: 'note' }, 'If you change the SMTP port, point your app at the new port too.'),
  )
  return el('details', { open }, el('summary', {}, 'Change ports'), form)
}

function render(s) {
  view.replaceChildren(...build(s).filter((c) => c != null))
}

function build(s) {
  switch (s.phase) {
    case 'starting':
      return [
        status('spin', 'Starting'),
        el('h1', {}, 'Starting Envry…'),
        el(
          'p',
          {},
          `Opening SMTP ${s.ports.smtp}, POP3 ${s.ports.pop3}, IMAP ${s.ports.imap} and the web UI on ${s.ports.http}` +
            (s.tls ? `, with secure connections on ${s.ports.smtps}, ${s.ports.pop3s} and ${s.ports.imaps}.` : '.'),
        ),
      ]
    case 'running':
      return [status('live', 'Live'), el('h1', {}, 'Envry is running'), el('p', {}, 'Opening the web UI…')]
    case 'conflict': {
      const retry = el('button', { class: 'primary', onclick: () => call('start_server', {}, retry) }, 'Try again')
      return [
        status('error', 'Offline'),
        el('h1', {}, s.busy.length > 1 ? 'Some ports are already in use' : 'A port is already in use'),
        el(
          'p',
          {},
          'Another mail catcher such as Mailpit or MailHog, or a second Envry, may be running. Stop it and try again, or run Envry on other ports.',
        ),
        el(
          'ul',
          { class: 'busy' },
          s.busy.map((b) => el('li', {}, el('span', { class: 'what' }, `${label(b.service)} port ${b.port}`), el('span', { class: 'who' }, whoHolds(b)))),
        ),
        el('div', { class: 'actions' }, retry),
        portsForm(s.ports, s.tls, { open: true, busy: s.busy }),
      ]
    }
    case 'failed': {
      const restart = el('button', { class: 'primary', onclick: () => call('start_server', {}, restart) }, 'Start again')
      const log = s.logPath ? el('button', { onclick: () => call('open_log', {}) }, 'Show log file') : null
      return [
        status('error', 'Offline'),
        el('h1', {}, 'Envry stopped'),
        el('p', {}, s.message),
        s.hint ? el('p', {}, s.hint) : null,
        s.log?.length ? el('pre', {}, s.log.join('\n')) : null,
        el('div', { class: 'actions' }, restart, log),
        portsForm(s.ports, s.tls),
      ]
    }
    default: {
      const start = el('button', { class: 'primary', onclick: () => call('start_server', {}, start) }, 'Start Envry')
      return [
        status('', 'Offline'),
        el('h1', {}, 'Envry is stopped'),
        el('p', {}, `Mail sent to port ${s.ports.smtp} is not being caught.`),
        el('div', { class: 'actions' }, start),
        portsForm(s.ports, s.tls),
      ]
    }
  }
}

// Title bar buttons. Closing hides the window; Envry keeps running in the tray.
function setupTitleBar() {
  const win = getCurrentWindow()
  const root = document.documentElement
  root.classList.toggle('mac', /Mac/.test(navigator.userAgent))
  document.getElementById('minimize').addEventListener('click', () => win.minimize())
  document.getElementById('maximize').addEventListener('click', () => win.toggleMaximize())
  document.getElementById('close').addEventListener('click', () => win.close())
  const sync = () =>
    win.isMaximized().then((max) => {
      root.classList.toggle('maximized', max)
      const button = document.getElementById('maximize')
      button.title = max ? 'Restore' : 'Maximize'
      button.setAttribute('aria-label', button.title)
    })
  sync()
  win.onResized(sync)
}

setupTitleBar()
listen('status', (e) => render(e.payload))
invoke('get_status').then(render)
