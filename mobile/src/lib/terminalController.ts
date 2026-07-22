// M3-auth-4A: Terminal bootstrap + WebSocket ticket lifecycle controller.
//
// Owns one terminal connection attempt. The reconnect hook installs after the
// DOM is fully loaded (so the daemon's connect() is defined), overrides
// connect() to request a fresh ticket from native, clears pending state on
// receipt, and calls the real connect() exactly once with the new ticket.
// Reconnect singleflight: simultaneous reconnect requests share one promise.

import { TokenManager } from './authClient';
import { authenticatedFetch } from './authTransport';
import { getWSTicket } from './wsTicket';

export interface TerminalBootstrap {
  html: string;
  baseUrl: string;
  ticket: string;
  sessionId: string;
}

// Globally unique connection ID — ensures different controller instances can never
// produce colliding attempt IDs (BLOCKER 3).
let _nextConnId = 1;

export class TerminalController {
  readonly connId = _nextConnId++;
  gen = 0;
  private reconnectFlight: Promise<{ attemptId: number; ticket?: string }> | null = null;

  async bootstrap(
    sessionId: string, tokenMgr: TokenManager, baseURL: string,
  ): Promise<{ attemptId: number; result?: TerminalBootstrap }> {
    this.cancel();
    const attemptId = ++this.gen;

    const htmlRes = await authenticatedFetch(
      `${baseURL}/term/?session=${encodeURIComponent(sessionId)}`, undefined, tokenMgr,
    );
    if (attemptId !== this.gen) return { attemptId };
    if (!htmlRes.ok) throw new Error(`bootstrap failed: ${htmlRes.status}`);
    let html = await htmlRes.text();

    // Get authoritative PTY size from the daemon so the mobile xterm mirrors
    // the host geometry (mobile does NOT resize the shared PTY).
    // TERM-G1: validate bounds — reject non-integer, zero, negative, or
    // implausibly large values. Invalid geometry falls through to viewport
    // fallback; the mobile viewer never authors PTY size.
    let ptySize = '';
    try {
      const szRes = await authenticatedFetch(`${baseURL}/term/size?session=${encodeURIComponent(sessionId)}`, undefined, tokenMgr);
      if (szRes.ok) {
        const sz = await szRes.json();
        const rows = Number(sz.rows);
        const cols = Number(sz.cols);
        if (Number.isInteger(rows) && Number.isInteger(cols) &&
            rows >= 1 && rows <= 1000 &&
            cols >= 1 && cols <= 2000) {
          ptySize = `window.__pokitPTYSize={rows:${rows},cols:${cols}};`;
        }
      }
    } catch {}

    const t = await getWSTicket(sessionId, tokenMgr, baseURL);
    if (attemptId !== this.gen) return { attemptId };

    // Inject ticket + reconnect bridge. The bridge installs AFTER the DOM
    // is fully loaded (DOMContentLoaded or fallback setTimeout) so the
    // daemon's real connect() is defined before we wrap it.
    const connId = this.connId;
    const ticketScript = `<script>${ptySize}
// TERM-C1-R3: paired-device HTML loses the ?session= query parameter
// when loaded via source={{html,baseUrl}}. Seed the exact session ID
// so the served-page control bridge can validate the daemon hello frame.
window.__pokitExpectedSession=${JSON.stringify(sessionId)};
(function(){
  var ticketA=${JSON.stringify(t.ticket)}, session=${JSON.stringify(sessionId)}, attemptId=${attemptId}, connId=${connId};
  var pendingReconnect=false, realConnectFn=null;
  // Build a WS URL with a ticket (ephemeral — ticket never enters history).
  function wsURL(tk){ var proto=location.protocol==='https:'?'wss:':'ws:'; var u=new URL('/term/ws',location.origin); u.protocol=proto; u.searchParams.set('session',session); u.searchParams.set('ticket',tk); return u.toString(); }
  // ── Phase 1: IMMEDIATE WebSocket constructor interception (before any page JS runs) ──
  // The daemon page builds the WS as:
  //   new WebSocket(protocol+location.host+"/term/ws"+location.search)
  // We intercept that to inject ticket A for the FIRST connection, then clear it.
  var ticketConsumed=false;
  var _origWS=window.WebSocket;
  window.WebSocket=function(url,protocols){
    if(typeof url==='string'&&url.indexOf('/term/ws')!==-1){
      var rawWS;
      if(!ticketConsumed){ ticketConsumed=true; rawWS=new _origWS(wsURL(ticketA),protocols); }
      else if(window._nextTicket){ var tk=window._nextTicket; window._nextTicket=null; rawWS=new _origWS(wsURL(tk),protocols); }
      else { rawWS=new _origWS(url,protocols); }
      // TERM-C1: the served daemon page owns the sole control dispatcher.
      // Bind this exact socket before it can receive frames; the page's binary
      // onmessage handler never parses or forwards control frames. This avoids
      // the former interceptor + page double delivery of hello/read_only/result.
      if(window.__pokitControlBridge&&window.__pokitControlBridge.bind){
        window.__pokitControlBridge.bind(rawWS);
      }
      // M3-auth-4A geometry poll lifecycle (Blocker D):
      //  - send an immediate poll on 'open' (not at construction);
      //  - start exactly ONE bounded interval, only after open;
      //  - clear it on close/error (and on WebView unmount / session switch,
      //    which destroys this JS context and every timer in it);
      //  - each reconnect socket owns its own timer (this wrapper runs per
      //    socket), so no timer survives its socket.
      // The daemon replies with a text geometry frame; the message listener
      // above resizes the local xterm. The mobile viewer only READS geometry
      // and never resizes the shared PTY.
      var pollTimer=null;
      function stopPoll(){ if(pollTimer!==null){ clearInterval(pollTimer); pollTimer=null; } }
      function poll(){ if(rawWS.readyState===1){ try{ rawWS.send('{"type":"geometry-poll"}'); }catch(e){} } else { stopPoll(); } }
      rawWS.addEventListener('open',function(){ stopPoll(); poll(); pollTimer=setInterval(poll,3000); });
      rawWS.addEventListener('close',stopPoll);
      rawWS.addEventListener('error',stopPoll);
      return rawWS;
    }
    return new _origWS(url,protocols);
  };
  window.WebSocket.prototype=_origWS.prototype;
  // ── Phase 2: reconnect wrapper (deferred until daemon's connect() is defined) ──
  function sendReconnect(){ try{window.ReactNativeWebView.postMessage(JSON.stringify({type:'pokit-reconnect-request',session:session,attemptId:attemptId,connId:connId}))}catch{} }
  function installReconnect(){
    if(window.__pokitBridgeInstalled) return; window.__pokitBridgeInstalled=true;
    realConnectFn=window.connect;
    window.connect=function(){ if(pendingReconnect) return; pendingReconnect=true; sendReconnect(); };
    window.addEventListener('message',function(e){
      try{
        var d=JSON.parse(e.data);
        if(d&&d.type==='pokit-ticket'&&typeof d.ticket==='string'&&/^[0-9a-f]{64}$/.test(d.ticket)){
          window._nextTicket=d.ticket;
          pendingReconnect=false;
          if(realConnectFn) realConnectFn();
          window._nextTicket=null;
        }
      }catch(ex){}
    });
  }
  if(document.readyState==='loading') document.addEventListener('DOMContentLoaded',installReconnect);
  else setTimeout(installReconnect,0);
})();
</script>`;
    html = html.replace(/<head[^>]*>/i, (m: string) => m + ticketScript);

    // TERM-C1: bootstrap geometry enters through the same served-page control
    // bridge as live WS geometry. No second handler may resize xterm directly.
    html = html.replace('</body>', `<script>if(window.__pokitPTYSize&&window.__pokitControlBridge){window.__pokitControlBridge.bootstrapGeometry(__pokitPTYSize.rows,__pokitPTYSize.cols)}</script></body>`);

    return { attemptId, result: { html, baseUrl: baseURL, ticket: t.ticket, sessionId } };
  }

  // reconnectTicket with singleflight. If a flight is already in progress for
  // the current generation, return it immediately without cancelling or
  // starting a new generation. Only start a new generation when there is no
  // flight (first caller or after completion).
  async reconnectTicket(
    sessionId: string, tokenMgr: TokenManager, baseURL: string,
  ): Promise<{ attemptId: number; ticket?: string }> {
    if (this.reconnectFlight) return this.reconnectFlight;

    // No flight in progress — cancel any stale work and start a new generation.
    this.gen++;
    const attemptId = this.gen;
    const flight = (async () => {
      const t = await getWSTicket(sessionId, tokenMgr, baseURL);
      if (attemptId !== this.gen) return { attemptId };
      return { attemptId, ticket: t.ticket };
    })();
    this.reconnectFlight = flight;
    try { return await flight; } finally { if (this.reconnectFlight === flight) this.reconnectFlight = null; }
  }

  cancel(): void { this.gen++; this.reconnectFlight = null; }
}

// shouldIssueReconnect is the authoritative guard for a WebView reconnect
// request: a fresh ticket is issued ONLY when the request matches the current
// controller instance (connId) and its current bootstrap generation
// (attemptId) for this exact session. Stale session/connId/attemptId signals
// (from an old controller, a previous session, or a superseded bootstrap)
// cause no issuance. Extracted so the stale-rejection contract is unit-tested
// without a full React render.
export function shouldIssueReconnect(
  data: any,
  ctx: { session: string; connId: number; attemptId: number },
): boolean {
  return !!data
    && data.type === 'pokit-reconnect-request'
    && data.session === ctx.session
    && data.connId === ctx.connId
    && data.attemptId === ctx.attemptId;
}
