import { teamInferencePath } from '@/api/playground-team'
import {
  nativeSnippetRequest,
  SnippetError,
  type SnippetInput,
  type SnippetLanguage,
} from './playground-snippet'

export interface TeamSnippetInput extends SnippetInput {
  source: 'team'
  teamId: string
}

export function teamSnippetRequest(input: TeamSnippetInput) {
  if (input.source !== 'team' || !/^[A-Za-z0-9_-]{1,30}$/.test(input.teamId))
    throw new SnippetError('team')
  const native = nativeSnippetRequest(input)
  const origin = new URL(native.url).origin
  return {
    origin,
    url: origin + teamInferencePath(input.teamId, input.protocol, input.model, input.stream),
    body: native.body,
    headers: { ...native.headers, Origin: origin },
  }
}

// Authentication never appears in generated arguments or files. cURL uses the
// same Python login bootstrap, then receives its private configuration on stdin.
function pythonAuthentication(origin: string) {
  return `import http.cookiejar
import json
import os
import re
import sys
import urllib.error
import urllib.request

ORIGIN = ${JSON.stringify(origin)}

class TeamExampleError(Exception):
    pass

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

def authenticate():
    email = os.environ.get("ROUTEX_EMAIL", "").strip().lower()
    password = os.environ.get("ROUTEX_PASSWORD", "")
    if not email or not password:
        raise TeamExampleError("Set ROUTEX_EMAIL and ROUTEX_PASSWORD before running.")
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPCookieProcessor(jar))
    login = urllib.request.Request(
        ORIGIN + "/api/v1/auth/login",
        data=json.dumps({"email": email, "password": password}).encode("utf-8"),
        headers={"Content-Type": "application/json", "Origin": ORIGIN},
        method="POST",
    )
    with opener.open(login, timeout=30) as response:
        if response.status == 202:
            raise TeamExampleError("Two-step login challenge required; no inference was sent.")
        if response.status != 200:
            raise TeamExampleError("Login did not return HTTP 200; no inference was sent.")
        issued = [value for value in response.headers.get_all("Set-Cookie", []) if value.startswith("routex_session=")]
        if len(issued) != 1 or not re.match(r"routex_session=[A-Za-z0-9_-]{43}(;|$)", issued[0]) or not re.search(r"(;|^)\\s*Path=/(;|$)", issued[0], re.I) or not re.search(r"(;|^)\\s*HttpOnly(;|$)", issued[0], re.I) or re.search(r"(;|^)\\s*Domain=", issued[0], re.I):
            raise TeamExampleError("Login did not issue a valid Session; no inference was sent.")
        if len(response.read(65537)) > 65536:
            raise TeamExampleError("Login response is invalid; no inference was sent.")
    sessions = [cookie for cookie in jar if cookie.name == "routex_session"]
    if len(sessions) != 1 or sessions[0].path != "/" or not re.fullmatch(r"[A-Za-z0-9_-]{43}", sessions[0].value):
        raise TeamExampleError("Login did not issue a valid Session; no inference was sent.")
    for cookie in list(jar):
        if cookie.name != "routex_session":
            jar.clear(cookie.domain, cookie.path, cookie.name)
    cookie = "routex_session=" + sessions[0].value
    current = urllib.request.Request(ORIGIN + "/api/v1/auth/session", headers={"Origin": ORIGIN}, method="GET")
    with opener.open(current, timeout=30) as response:
        if response.status != 200:
            raise TeamExampleError("Current Session did not return HTTP 200; no inference was sent.")
        raw = response.read(65537)
        if len(raw) > 65536:
            raise TeamExampleError("Current Session is invalid; no inference was sent.")
        try:
            session = json.loads(raw)
        except (ValueError, UnicodeError):
            raise TeamExampleError("Current Session is invalid; no inference was sent.") from None
    user = session.get("user") if isinstance(session, dict) else None
    csrf = session.get("csrf_token") if isinstance(session, dict) else None
    if not isinstance(user, dict) or not isinstance(user.get("id"), str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,30}", user["id"]) or user.get("email") != email or user.get("role") not in ("member", "admin") or not isinstance(csrf, str) or not re.fullmatch(r"[a-f0-9]{64}", csrf):
        raise TeamExampleError("Current Session is invalid; no inference was sent.")
    return opener, cookie, csrf

`
}

function pythonProgram(input: TeamSnippetInput, curl: boolean) {
  const request = teamSnippetRequest(input)
  const captured = JSON.stringify({
    url: request.url,
    headers: request.headers,
    body: request.body,
  })
  const invocation = curl
    ? `    import subprocess
    config = ["silent", "show-error", "include", "no-buffer", "suppress-connect-headers", "connect-timeout = 30", "max-time = 300", 'request = "POST"', "url = " + json.dumps(request["url"])]
    headers["Cookie"] = cookie
    headers["Expect"] = ""
    for name, value in headers.items():
        config.append("header = " + json.dumps(name + ": " + value))
    config.append("data-binary = " + json.dumps(json.dumps(request["body"], ensure_ascii=False), ensure_ascii=False))
    child_env = dict(os.environ)
    child_env.pop("ROUTEX_EMAIL", None)
    child_env.pop("ROUTEX_PASSWORD", None)
    with subprocess.Popen(["curl", "--disable", "--config", "-"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=child_env) as child:
        try:
            child.stdin.write(("\\n".join(config) + "\\n").encode("utf-8"))
            child.stdin.close()
            while True:
                status_line = child.stdout.readline(8193)
                match = re.fullmatch(rb"HTTP/[^ ]+ ([0-9]{3})[^\\r\\n]*\\r?\\n", status_line)
                if not match:
                    raise TeamExampleError("Native HTTP response is invalid; no request was retried.")
                status = int(match.group(1))
                header_bytes = len(status_line)
                while True:
                    line = child.stdout.readline(8193)
                    header_bytes += len(line)
                    if not line or len(line) > 8192 or header_bytes > 65536:
                        raise TeamExampleError("Native HTTP headers are invalid; no request was retried.")
                    if line in (b"\\r\\n", b"\\n"):
                        break
                if status >= 200:
                    break
            if not 200 <= status < 300:
                raise TeamExampleError("Native request denied or redirected; no request was retried.")
            while True:
                chunk = child.stdout.read1(8192)
                if not chunk:
                    break
                sys.stdout.buffer.write(chunk)
                sys.stdout.buffer.flush()
            if child.wait() != 0:
                raise TeamExampleError("Native transport failed; no request was retried.")
        finally:
            if child.poll() is None:
                child.kill()
            child.wait()
`
    : `    native = urllib.request.Request(request["url"], data=json.dumps(request["body"]).encode("utf-8"), headers=headers, method="POST")
    with opener.open(native, timeout=300) as response:
        if not 200 <= response.status < 300:
            raise TeamExampleError("Native request denied or redirected; no request was retried.")
        while True:
            chunk = response.read1(8192)
            if not chunk:
                break
            sys.stdout.buffer.write(chunk)
            sys.stdout.buffer.flush()
`
  return `${pythonAuthentication(request.origin)}def main():
    opener, cookie, csrf = authenticate()
    request = json.loads(${JSON.stringify(captured)})
    headers = request["headers"]
    headers["X-CSRF-Token"] = csrf
${invocation}
try:
    main()
except TeamExampleError as error:
    print(str(error), file=sys.stderr)
    sys.exit(1)
except Exception:
    print("Team request failed; check login, current Session and native transport. No request was retried.", file=sys.stderr)
    sys.exit(1)
`
}

export function buildTeamPlaygroundSnippet(
  input: TeamSnippetInput,
  language: SnippetLanguage,
): string {
  const request = teamSnippetRequest(input)
  if (language === 'curl') {
    // A quoted heredoc preserves arbitrary prompt text. Pick a delimiter which
    // cannot occur in the captured program, including user-supplied newlines.
    const program = pythonProgram(input, true)
    let delimiter = 'ROUTEX_TEAM_EXAMPLE'
    while (program.split('\n').includes(delimiter)) delimiter += '_'
    return `# Requires cURL and Python 3. Authentication stays in memory; no browser cookies are copied.
python3 - <<'${delimiter}'
${program}${delimiter}
`
  }
  if (language === 'python')
    return '# Python 3; standard library only.\n' + pythonProgram(input, false)
  if (language !== 'javascript') throw new SnippetError('language')
  return `// Node.js ES module (native fetch). Authentication stays in memory.
const origin = ${JSON.stringify(request.origin)};
function fail(message) { console.error(message); process.exit(1); }
async function currentSession(response) {
  if (response.status !== 200) fail("Current Session did not return HTTP 200; no inference was sent.");
  const chunks = [];
  let size = 0;
  for await (const chunk of response.body) {
    size += chunk.length;
    if (size > 65536) fail("Current Session is invalid; no inference was sent.");
    chunks.push(chunk);
  }
  let session;
  try { session = JSON.parse(Buffer.concat(chunks).toString("utf8")); }
  catch { fail("Current Session is invalid; no inference was sent."); }
  return session;
}
try {
  const email = (process.env.ROUTEX_EMAIL ?? "").trim().toLowerCase();
  const password = process.env.ROUTEX_PASSWORD ?? "";
  if (!email || !password) fail("Set ROUTEX_EMAIL and ROUTEX_PASSWORD before running.");
  const login = await fetch(origin + "/api/v1/auth/login", {
    method: "POST", redirect: "error", credentials: "omit", signal: AbortSignal.timeout(30000),
    headers: { "Content-Type": "application/json", Origin: origin },
    body: JSON.stringify({ email, password }),
  });
  if (login.status === 202) fail("Two-step login challenge required; no inference was sent.");
  if (login.status !== 200) fail("Login did not return HTTP 200; no inference was sent.");
  const issued = login.headers.getSetCookie().filter(value => /^routex_session=/.test(value));
  if (issued.length !== 1 || !/^routex_session=[A-Za-z0-9_-]{43}(?:;|$)/.test(issued[0]) ||
      !/(?:^|;)\\s*Path=\\/(?:;|$)/i.test(issued[0]) || !/(?:^|;)\\s*HttpOnly(?:;|$)/i.test(issued[0]) || /(?:^|;)\\s*Domain=/i.test(issued[0]))
    fail("Login did not issue a valid Session; no inference was sent.");
  const cookie = issued[0].split(";", 1)[0];
  await login.body?.cancel();
  const session = await currentSession(await fetch(origin + "/api/v1/auth/session", {
    method: "GET", redirect: "error", credentials: "omit", cache: "no-store", signal: AbortSignal.timeout(30000),
    headers: { Cookie: cookie, Origin: origin },
  }));
  if (!session || !session.user || typeof session.user.id !== "string" || !/^[A-Za-z0-9_-]{1,30}$/.test(session.user.id) ||
      session.user.email !== email || !["member", "admin"].includes(session.user.role) ||
      typeof session.csrf_token !== "string" || !/^[a-f0-9]{64}$/.test(session.csrf_token))
    fail("Current Session is invalid; no inference was sent.");
  const response = await fetch(${JSON.stringify(request.url)}, {
    method: "POST", redirect: "error", credentials: "omit", signal: AbortSignal.timeout(300000),
    headers: { ...${JSON.stringify(request.headers)}, Cookie: cookie, "X-CSRF-Token": session.csrf_token },
    body: JSON.stringify(${JSON.stringify(request.body, null, 2)}),
  });
  if (!response.ok) fail("Native request denied or redirected; no request was retried.");
  for await (const chunk of response.body) process.stdout.write(chunk);
} catch {
  fail("Team request failed; check login, current Session and native transport. No request was retried.");
}
`
}
