import React from 'react';
import { Text, View } from 'react-native';
export type CockpitOrigin = { provider: string; sessionId: string; runtimeId: string; generation: string; model?: string; snapshotId?: string; evidenceRef?: string };
export function CockpitOriginView({ origin }: { origin: CockpitOrigin }) { return <View><Text>Origin · {origin.provider} · {origin.sessionId} · {origin.runtimeId} · gen {origin.generation}</Text><Text>{origin.model ? `Model ${origin.model} · ` : ''}{origin.snapshotId ? `Snapshot ${origin.snapshotId} · ` : ''}{origin.evidenceRef ? `Evidence ${origin.evidenceRef}` : 'Evidence unavailable'}</Text></View>; }
