// S1-E (E4): cross-language DTO compatibility. This golden string is byte-identical
// to `s1eDTOGolden` in companion-daemon/internal/term/s1e_correctness_test.go —
// the exact wire shape the daemon marshals. The PRODUCTION mobile validator must
// decode it (frozen T0 version + full field set). Keep the two goldens in lockstep.

import { validateAgentActivity, ACTIVITY_CONTRACT_VERSION } from '../src/lib/agentActivity';

const GOLDEN = '{"contractVersion":"t0.1","status":"working","provenance":"native_log","confidence":0.9,"degraded":false,"observedAt":"2026-07-13T00:00:00Z","stale":false}';

describe('agentActivity DTO golden (cross-language compatibility)', () => {
  it('the production validator decodes the daemon golden with the frozen T0 version', () => {
    const a = validateAgentActivity(JSON.parse(GOLDEN));
    expect(a).not.toBeNull();
    expect(a!.contractVersion).toBe('t0.1');
    expect(ACTIVITY_CONTRACT_VERSION).toBe('t0.1');
    expect(a!.status).toBe('working');
    expect(a!.provenance).toBe('native_log');
    expect(a!.confidence).toBeCloseTo(0.9);
    expect(a!.degraded).toBe(false);
    expect(a!.observedAt).toBe('2026-07-13T00:00:00Z');
    expect(a!.stale).toBe(false);
  });

  it('rejects the golden if the daemon ever emits a non-t0.1 version', () => {
    const drifted = JSON.parse(GOLDEN);
    drifted.contractVersion = 't0.2';
    expect(validateAgentActivity(drifted)).toBeNull();
  });
});
