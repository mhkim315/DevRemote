// C3D-B: extracted pure error classifier and onResolved gate for
// deterministic testing of the presentation boundary without requiring
// a full React Native render.

export function classifyApprovalError(statusCode: number | undefined): string {
  if (statusCode === 401 || statusCode === 403) {
    return 'Device not authorized — pair this device';
  } else if (statusCode === 409) {
    return 'No longer current — refresh';
  } else if (statusCode === 410) {
    return 'Approval expired';
  } else if (statusCode === 502) {
    return "Couldn't deliver — resolve in the terminal";
  } else if (statusCode === 400) {
    return 'Action not accepted';
  }
  return 'Network error — tap an action to retry';
}

export function resolveCallSucceeded(outcome: string | undefined, action: string | undefined): boolean {
  return (outcome === 'accepted' || outcome === 'already_accepted') && !!action;
}
