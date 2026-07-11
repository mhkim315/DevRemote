// M3-auth-4A: Terminal bootstrap + WebSocket ticket lifecycle controller.
//
// Owns one terminal connection attempt: fetches /term/ HTML (bearer), obtains
// a one-time WS ticket, injects it into the bootstrap URL so the page's JS
// passes it to /term/ws, and provides a reconnect bridge for ticket rotation.
// Stale attempts are discarded by a monotonically-increasing generation counter.

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
  private abort: AbortController | null = null;

  // bootstrap fetches the /term/ HTML, obtains a WS ticket, and returns an
  // inline source ready for WebView loading. The returned html includes a
  // <script> that injects the ticket, so the page's JS passes it to /term/ws.
  async bootstrap(
    sessionId: string, tokenMgr: TokenManager, baseURL: string,
  ): Promise<TerminalBootstrap> {
    // Cancel any in-flight attempt and start a new generation.
    this.cancel();
    const gen = ++this.gen;
    const ac = new AbortController();
    this.abort = ac;

    // 1. Fetch /term/ HTML with device bearer.
    const htmlRes = await this.fetchWithAbort(
      ac, gen, `${baseURL}/term/?session=${encodeURIComponent(sessionId)}`, { signal: ac.signal }, tokenMgr,
    );
    if (!htmlRes.ok) throw new Error(`terminal bootstrap failed: ${htmlRes.status}`);
    let html = await htmlRes.text();

    // 2. Obtain a one-time WS ticket.
    const t = await getWSTicket(sessionId, tokenMgr, baseURL);

    // 3. Guard: another bootstrap may have started while we awaited.
    if (gen !== this.gen) throw new Error('stale bootstrap attempt');

    // 4. Inject ticket into the HTML BEFORE the page's JS opens a WS.
    //    The terminal page builds the WS URL as:
    //      new WebSocket(protocol+location.host+"/term/ws"+location.search)
    //    so we inject ?session=X&ticket=YYY into location.search before
    //    connect() runs. The script also registers a native-to-WebView
    //    message listener for ticket rotation on reconnect.
    const ticketScript = `<script>
(function(){
  var u=new URL(location.href);
  u.searchParams.set('session','${sessionId}');
  u.searchParams.set('ticket','${t.ticket}');
  window.history.replaceState({},'',u.toString());
  // Reconnect bridge: the native layer can postMessage a new ticket.
  window.__pokitSetTicket=function(t){ var p=new URL(location.href);p.searchParams.set('ticket',t);window.history.replaceState({},'',p.toString()); };
  window.addEventListener('message',function(e){ if(e.data&&e.data.type==='pokit-ticket'){ window.__pokitSetTicket(e.data.ticket); } });
  window.__pokitHandshakeUrl='/term/ws'+u.search;
})();
</script>`;
    html = html.replace(/<head[^>]*>/i, (m: string) => m + ticketScript);

    return { html, baseUrl: baseURL, ticket: t.ticket, sessionId };
  }

  // reconnectTicket fetches a fresh ticket for reuse by the WebView after a
  // WebSocket disconnect. The previous ticket was consumed.
  async reconnectTicket(
    sessionId: string, tokenMgr: TokenManager, baseURL: string,
  ): Promise<string> {
    const gen = ++this.gen;
    this.cancel();
    const ac = new AbortController();
    this.abort = ac;
    const t = await getWSTicket(sessionId, tokenMgr, baseURL);
    if (gen !== this.gen) throw new Error('stale reconnect');
    return t.ticket;
  }

  // isCurrent reports whether gen matches the most recent attempt.
  isCurrent(gen: number): boolean { return gen === this.gen; }

  // cancel aborts any pending request and prevents stale completion.
  cancel(): void {
    if (this.abort) { this.abort.abort(); this.abort = null; }
    this.gen++;
  }

  private async fetchWithAbort(
    ac: AbortController, gen: number, url: string, init: RequestInit, tokenMgr: TokenManager,
  ): Promise<Response> {
    const token = await tokenMgr.getValidToken();
    if (gen !== this.gen || ac.signal.aborted) throw new Error('aborted');
    try {
      return await authenticatedFetch(url, { ...init, headers: { ...(init.headers as Record<string, string> || {}), Authorization: `Bearer ${token}` } }, tokenMgr);
    } catch (e) {
      if (ac.signal.aborted) throw new Error('aborted');
      throw e;
    }
  }
}
