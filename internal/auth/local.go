package auth

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// forwardingHeaders are the headers a reverse proxy adds when it passes a
// request on. Only their presence is ever read — see LocalRequest.
//
// The list is deliberately wider than "the ones that carry an address". A proxy
// that sets only `X-Forwarded-Host` — rewriting the Host to the upstream while
// recording the original — passes both of the other conditions, and the first
// version of this list judged such a caller local. Every entry here can only
// ever make a request *less* local, so an over-broad list costs nothing and a
// missing one is a hole.
var forwardingHeaders = []string{
	http.CanonicalHeaderKey("X-Forwarded-For"),
	http.CanonicalHeaderKey("X-Forwarded-Host"),
	http.CanonicalHeaderKey("X-Forwarded-Proto"),
	http.CanonicalHeaderKey("X-Forwarded-Port"),
	http.CanonicalHeaderKey("X-Real-IP"),
	http.CanonicalHeaderKey("Forwarded"),
}

// LocalRequest reports whether a request came from a client on the machine this
// server runs on, acting for the person sitting at it.
//
// It is the predicate behind "this is the single-binary local mode": with no
// api_token and no accounts configured, a write is accepted from a local caller
// and refused from every other one, and the `default_user` auto-login token is
// handed out on the same terms.
//
// Four conditions. The first two answer "is the peer on this machine"; the last
// two answer "is this the operator's own client, or a web page using their
// browser as a proxy" — which loopback alone cannot tell apart, and which is the
// question that matters once loopback becomes an authorisation rather than a
// description.
//
//  1. No forwarding header. `deploy/nginx/` ships a same-host reverse proxy
//     template, and behind one every remote caller arrives from 127.0.0.1 — so
//     the address on its own would call the whole internet local, in exactly the
//     configuration this repository tells people to run. A proxy appends one of
//     these and a client cannot remove what the proxy adds.
//
//  2. RemoteAddr is a loopback address — the peer of the connection the kernel
//     accepted, which a caller cannot set.
//
//  3. The Host header names a loopback address. A proxy that passes the public
//     name through fails this even when it sets no forwarding header. It is a
//     partial defence and deliberately so: nginx's default `proxy_set_header
//     Host $proxy_host` rewrites Host to the upstream address, so a bare
//     `proxy_pass` still passes. Nothing at the HTTP layer distinguishes an L4
//     forwarder on the same host from a genuine local client; only binding the
//     listener to loopback does, and that is a deployment decision rather than
//     this predicate's to make.
//
//  4. Origin, when present, names a loopback address. Without this the whole
//     predicate is bypassable by any web page the operator visits:
//     corsMiddleware answers with `Access-Control-Allow-Origin: *`, so a script
//     on evil.com can POST to 127.0.0.1 from the operator's own browser — a
//     loopback peer, no forwarding header — and read the reply. Requests with no
//     Origin at all (curl, an MCP client, the server's own web UI on a
//     same-origin GET) are unaffected; this only ever refuses a caller that
//     announced it came from somewhere else. `internal/api/ws/handler.go` asks
//     the same question of a WebSocket handshake for the same reason.
//
// Only the presence of the forwarding headers is read, never their value. What
// they claim is a separate question with a separate trust model, and this
// predicate must not inherit it: a caller who forges `X-Real-IP: 127.0.0.1` is
// made less local by doing so, not more.
func LocalRequest(r *http.Request) bool {
	for _, h := range forwardingHeaders {
		if _, present := r.Header[h]; present {
			return false
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// No port to split: take the whole value, which is what a test server
		// or a unix socket leaves behind.
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return false
	}

	if !loopbackHost(r.Host) {
		return false
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !loopbackHost(u.Host) {
			// A malformed Origin, or the literal "null" a sandboxed iframe
			// sends, lands here too. Both are "not the operator's own client".
			return false
		}
	}

	return true
}

// loopbackHost reports whether a `host[:port]` names this machine.
//
// `localhost` is accepted by name: it is what a person types, and resolving it
// is the operating system's job, not this function's.
func loopbackHost(hostport string) bool {
	if hostport == "" {
		return false
	}
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")

	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
