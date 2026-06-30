import React, {useState} from 'react';
import HomeScreen from './src/screens/HomeScreen';
import SessionScreen from './src/screens/SessionScreen';

export default function App() {
  const [wsUrl, setWsUrl] = useState<string | null>(null);

  if (wsUrl) {
    return (
      <SessionScreen wsUrl={wsUrl} onDisconnect={() => setWsUrl(null)} />
    );
  }

  return <HomeScreen onConnect={url => setWsUrl(url)} />;
}
