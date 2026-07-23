import React, { useEffect, useState } from 'react';
import { ScrollView, Text, View } from 'react-native';
import { CockpitOrigin, CockpitOriginView } from '../components/CockpitOrigin';
import { getCockpit } from '../lib/client';
export type CockpitItem = { kind: 'runtime'|'approval'|'input'|'notification'|'validation'|'evidence'; state: string; summary: string; stale?: boolean; origin: CockpitOrigin };
export default function CockpitScreen({ token }: { token?: string }) {
  const [items, setItems] = useState<CockpitItem[]>([]); const [loading, setLoading] = useState(true); const [error, setError] = useState('');
  useEffect(() => { let live=true; getCockpit(token).then((state: any) => { if(live) setItems([...(state.sessions||[]),...(state.approvals||[]),...(state.findings||[]),...(state.notifications||[])]); }).catch(e=>live&&setError(String(e.message||e))).finally(()=>live&&setLoading(false)); return()=>{live=false}; }, [token]);
  return <ScrollView><Text>Operational Cockpit</Text><Text>Read-only projection — actions remain with their authoritative services.</Text>{loading&&<Text>Loading cockpit…</Text>}{error!==''&&<Text>Unable to load cockpit: {error}</Text>}{!loading&&!error&&items.length===0&&<Text>No operational items.</Text>}{items.map((item, index) => <View key={`${item.kind}-${index}`}><Text>{item.kind}: {item.state}{item.stale ? ' · STALE' : ''}</Text><Text>{item.summary}</Text><CockpitOriginView origin={item.origin}/></View>)}</ScrollView>;
}
