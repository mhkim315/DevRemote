import React from 'react';
import { ScrollView, Text, View } from 'react-native';
import { CockpitOrigin, CockpitOriginView } from '../components/CockpitOrigin';
export type CockpitItem = { kind: 'runtime'|'approval'|'input'|'notification'|'validation'|'evidence'; state: string; summary: string; stale?: boolean; origin: CockpitOrigin };
export default function CockpitScreen({ items = [] }: { items?: CockpitItem[] }) { return <ScrollView><Text>Operational Cockpit</Text><Text>Read-only projection — actions remain with their authoritative services.</Text>{items.map((item, index) => <View key={`${item.kind}-${index}`}><Text>{item.kind}: {item.state}{item.stale ? ' · STALE' : ''}</Text><Text>{item.summary}</Text><CockpitOriginView origin={item.origin}/></View>)}</ScrollView>; }
