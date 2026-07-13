// S1-D (D7): the request guard drops stale / out-of-order / post-unmount responses
// so a previous request never renders after a session switch or unmount.

import { createRequestGuard } from '../src/lib/requestGuard';

describe('createRequestGuard', () => {
  it('only the latest request is current (deferred A→B switch drops A)', () => {
    const g = createRequestGuard();
    const reqA = g.begin();       // request A starts
    const reqB = g.begin();       // request B starts (switch/refresh)
    // B resolves first, then A resolves late:
    expect(g.isCurrent(reqB)).toBe(true);   // B commits
    expect(g.isCurrent(reqA)).toBe(false);  // late A is dropped, not rendered
  });

  it('a response resolving after unmount is dropped', () => {
    const g = createRequestGuard();
    const req = g.begin();
    g.cancel();                    // component unmounts
    expect(g.isCurrent(req)).toBe(false);
  });

  it('a normal single request commits', () => {
    const g = createRequestGuard();
    const req = g.begin();
    expect(g.isCurrent(req)).toBe(true);
  });

  it('stays cancelled: no later token is current after unmount', () => {
    const g = createRequestGuard();
    g.cancel();
    const req = g.begin();
    expect(g.isCurrent(req)).toBe(false);
  });
});
