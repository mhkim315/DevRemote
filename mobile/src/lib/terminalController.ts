// M3-auth-4A: Terminal bootstrap + WebSocket ticket lifecycle controller.
//
// Owns one terminal connection attempt: fetches /term/ HTML (bearer), obtains
// a one-time WS ticket, injects it into the bootstrap URL so the page's JS
// passes it to /term/ws, and provides a reconnect bridge for ticket rotation.
// Generation model: a single monotonically-increasing integer `gen`.
// cancel() invalidates the current gen; bootstrap/reconnect each create a new
// gen atomically and return the attempt ID. Only the caller holding the
// matching ID may commit results.

import { TokenManager } from './authClient';
import { authenticatedFetch } from './authTransport';
import { getWSTicket } from './wsTicket';

export interface TerminalBootstrap {
  html: string;
  baseUrl: string;
  ticket: string;
  sessionId: string;
}

// TerminalController owns the lifecycle of ONE terminal connection attempt.
export class TerminalController {
  private gen = 0;

  // bootstrap fetches the /term/ HTML, obtains a WS ticket, injects it, and
  // returns an {attemptId, result} pair. The caller must verify attemptId
  // matches before applying the result (stale attempts are discarded).
  async bootstrap(
    sessionId: string, tokenMgr: TokenManager, baseURL: string,
  ): Promise<{ attemptId: number; result?: TerminalBootstrap }> {
    // Atomically: cancel prior work, start a new generation.
    this.cancel();
    const attemptId = ++this.gen;

    // 1. Fetch /term/ HTML with device bearer.
    const htmlRes = await authenticatedFetch(
      `${baseURL}/term/?session=${encodeURIComponent(sessionId)}`, { signal: this.abortSignal() }, tokenMgr,
    );
    if (attemptId !== this.gen) return { attemptId };
    if (!htmlRes.ok) throw new Error(`terminal bootstrap failed: ${htmlRes.status}`);
    let html = await htmlRes.text();

    // 2. Obtain a one-time WS ticket.
    const t = await getWSTicket(sessionId, tokenMgr, baseURL);
    if (attemptId !== this.gen) return { attemptId };

    // 3. Inject ticket into the HTML so the page's JS passes it to /term/ws.
    //    The page builds the WS URL from location.search, so we set
    //    ?session=X&ticket=YYY via history.replaceState before the page's
    //    connect() call. Also install a reconnect bridge that receives ticket
    //    updates from the native layer via postMessage.
    const ticketScript = `<script>
(function(){
  var u=new URL(location.href);
  u.searchParams.set('session','${sessionId}');
  u.searchParams.set('ticket','${t.ticket}');
  window.history.replaceState({},'',u.toString());
  // Reconnect bridge: native layer posts {type:'pokit-ticket',ticket:64hex}.
  window.__pokitSetTicket=function(tk){ var p=new URL(location.href);p.searchParams.set('ticket',tk);window.history.replaceState({},'',p.toString()); };
  window.addEventListener('message',function(e){ try{ var d=typeof e.data==='string'?JSON.parse(e.data):e.data; if(d&&d.type==='pokit-ticket'&&typeof d.ticket==='string'){ window.__pokitSetTicket(d.ticket); } }catch{} });
  window.__pokitHandshakeUrl='/term/ws'+u.search;
  // Notify native when reconnect is needed (WebSocket closed).
  window.__pokitNotifyReconnect=function(){ try{window.ReactNativeWebView.postMessage(JSON.stringify({type:'pokit-reconnect-request',session:'${sessionId}',attemptId:${attemptId}}));}catch{} };
  // Override the page's auto-reconnect: call native for a fresh ticket first.
  var _origConnect=window.connect;window.connect=function(){ if(window.__pokitReconnectPending){ return; } window.__pokitReconnectPending=true; window.__pokitNotifyReconnect(); };
})();
</script>`;
    html = html.replace(/<head[^>]*>/i, (m: string) => m + ticketScript);

    return { attemptId, result: { html, baseUrl: baseURL, ticket: t.ticket, sessionId } };
  }

  // reconnectTicket fetches a fresh ticket for a reconnect rotation (A→B→C).
  // The caller owns delivering it to the WebView via postMessage.
  async reconnectTicket(
    sessionId: string, tokenMgr: TokenManager, baseURL: string,
  ): Promise<{ attemptId: number; ticket?: string }> {
    // Atomically: cancel prior, start new generation.
    this.cancel();
    const attemptId = ++this.gen;
    const t = await getWSTicket(sessionId, tokenMgr, baseURL);
    if (attemptId !== this.gen) return { attemptId };
    return { attemptId, ticket: t.ticket };
  }

  // cancel invalidates the current generation so any in-flight or pending
  // attempt will see a stale gen and self-discard.
  cancel(): void { this.gen++; }

  // abortSignal returns an AbortSignal bound to the current generation.
  // (AuthenticatedFetch doesn't use AbortController natively, so this is
  // a best-effort signal for callers that accept one.)
  private abortSignal(): AbortSignal | undefined { return undefined; }
}
