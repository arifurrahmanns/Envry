// Builds the envry server binary that the desktop app runs as its sidecar.
//
// Tauri runs this before `tauri dev` and `tauri build` and sets
// TAURI_ENV_TARGET_TRIPLE; Tauri expects the binary at
// src-tauri/binaries/envry-<target triple>[.exe]. The web UI is built first
// because the server embeds it. Set ENVRY_SKIP_WEB=1 to reuse an existing
// web build, and ENVRY_VERSION to stamp a release version.
import { spawnSync } from 'node:child_process'
import { existsSync, mkdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const desktop = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const root = resolve(desktop, '..')
const outDir = join(desktop, 'src-tauri', 'binaries')

const targets = {
  'x86_64-pc-windows-msvc': ['windows', 'amd64'],
  'aarch64-pc-windows-msvc': ['windows', 'arm64'],
  'x86_64-apple-darwin': ['darwin', 'amd64'],
  'aarch64-apple-darwin': ['darwin', 'arm64'],
  'x86_64-unknown-linux-gnu': ['linux', 'amd64'],
  'aarch64-unknown-linux-gnu': ['linux', 'arm64'],
}

function run(cmd, args, opts = {}) {
  const r = spawnSync(cmd, args, { stdio: 'inherit', shell: process.platform === 'win32', ...opts })
  if (r.status !== 0) {
    console.error(`sidecar: ${cmd} ${args.join(' ')} failed`)
    process.exit(r.status ?? 1)
  }
}

function output(cmd, args) {
  const r = spawnSync(cmd, args, { encoding: 'utf8', cwd: root })
  return r.status === 0 ? r.stdout.trim() : ''
}

function hostTriple() {
  const t = output('rustc', ['--print', 'host-tuple'])
  if (!t) {
    console.error('sidecar: set TAURI_ENV_TARGET_TRIPLE or install Rust')
    process.exit(1)
  }
  return t
}

function version() {
  if (process.env.ENVRY_VERSION) return process.env.ENVRY_VERSION
  return output('git', ['describe', '--tags', '--always', '--dirty']) || 'dev'
}

function goBuild(goos, goarch, out) {
  const pkg = 'github.com/arifurrahmanns/envry/internal/version'
  const ldflags = [
    '-s -w',
    `-X ${pkg}.Version=${version()}`,
    `-X ${pkg}.Commit=${output('git', ['rev-parse', '--short', 'HEAD']) || 'none'}`,
  ].join(' ')
  run('go', ['build', '-trimpath', `-ldflags=${ldflags}`, '-o', out, './cmd/envry'], {
    cwd: root,
    shell: false,
    env: { ...process.env, CGO_ENABLED: '0', GOOS: goos, GOARCH: goarch, GOTOOLCHAIN: process.env.GOTOOLCHAIN || 'local' },
  })
}

const triple = process.env.TAURI_ENV_TARGET_TRIPLE || hostTriple()

if (process.env.ENVRY_SKIP_WEB !== '1') {
  const web = join(root, 'web')
  if (!existsSync(join(web, 'node_modules'))) run('npm', ['ci'], { cwd: web })
  run('npm', ['run', 'build'], { cwd: web })
}

mkdirSync(outDir, { recursive: true })

if (triple === 'universal-apple-darwin') {
  // The per-architecture binaries stay: Cargo builds each half separately
  // and tauri-build looks for the sidecar of the triple it is compiling.
  const parts = ['x86_64-apple-darwin', 'aarch64-apple-darwin'].map((t) => {
    const out = join(outDir, `envry-${t}`)
    goBuild(...targets[t], out)
    return out
  })
  run('lipo', ['-create', '-output', join(outDir, `envry-${triple}`), ...parts], { shell: false })
} else {
  const target = targets[triple]
  if (!target) {
    console.error(`sidecar: unsupported target ${triple}`)
    process.exit(1)
  }
  const ext = target[0] === 'windows' ? '.exe' : ''
  goBuild(...target, join(outDir, `envry-${triple}${ext}`))
}
console.log(`sidecar: built envry for ${triple}`)
