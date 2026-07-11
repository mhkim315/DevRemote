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

export class TerminalController {
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

    const t = await getWSTicket(sessionId, tokenMgr, baseURL);
    if (attemptId !== this.gen) return { attemptId };

    // Inject ticket + reconnect bridge. The bridge installs AFTER the DOM
    // is fully loaded (DOMContentLoaded or fallback setTimeout) so the
    // daemon's real connect() is defined before we wrap it.
    const ticketScript = `<script>
(function(){
  var ticket=${JSON.stringify(t.ticket)}, session=${JSON.stringify(sessionId)}, attemptId=${attemptId};
  var u=new URL(location.href); u.searchParams.set('session',session); u.searchParams.set('ticket',ticket); window.history.replaceState({},'',u.toString());
  var pending=false, realConnect=null;
  function sendReconnect(){ try{window.ReactNativeWebView.postMessage(JSON.stringify({type:'pokit-reconnect-request',session:session,attemptId:attemptId}))}catch{} }
  function install(){
    if(window.__pokitBridgeInstalled) return; window.__pokitBridgeInstalled=true;
    realConnect=window.connect; window.connect=function(){ if(pending) return; pending=true; sendReconnect(); };
    // Listen for fresh ticket from native, validate it, then connect.
    window.addEventListener('message',function(e){ try{ var d=JSON.parse(e.data); if(d&&d.type==='pokit-ticket'&&typeof d.ticket==='string'&&/^[0-9a-f]{64}$/.test(d.ticket)){ u.searchParams.set('ticket',d.ticket); window.history.replaceState({},'',u.toString()); pending=false; if(realConnect) realConnect(); } }catch{} });
  }
  if(document.readyState==='loading') document.addEventListener('DOMContentLoaded',install); else setTimeout(install,0);
})();
</script>`;
    html = html.replace(/<head[^>]*>/i, (m: string) => m + ticketScript);

    return { attemptId, result: { html, baseUrl: baseURL, ticket: t.ticket, sessionId } };
  }

  // reconnectTicket with singleflight. Simultaneous calls for the same
  // attempt share one in-flight promise.
  async reconnectTicket(
    sessionId: string, tokenMgr: TokenManager, baseURL: string,
  ): Promise<{ attemptId: number; ticket?: string }> {
    this.cancel();
    const attemptId = ++this.gen;
    if (this.reconnectFlight) {
      const prev = await this.reconnectFlight;
      if (prev.attemptId === attemptId) return prev;
    }
    const flight = (async () => {
      const t = await getWSTicket(sessionId, tokenMgr, baseURL);
      if (attemptId !== this.gen) return { attemptId };
      return { attemptId, ticket: t.ticket };
    })();
    this.reconnectFlight = flight;
    const result = await flight;
    this.reconnectFlight = null;
    return result;
  }

  cancel(): void { this.gen++; this.reconnectFlight = null; }
}
