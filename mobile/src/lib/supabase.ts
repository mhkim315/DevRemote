import 'react-native-url-polyfill/auto';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { createClient } from '@supabase/supabase-js';

const supabaseUrl = 'https://vcozwcrbzjlahhctohub.supabase.co';
const supabaseAnonKey = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6InZjb3p3Y3JiempsYWhoY3RvaHViIiwicm9sZSI6ImFub24iLCJpYXQiOjE3ODMwNzQyMTksImV4cCI6MjA5ODY1MDIxOX0.EQ-mJ5tnYilGsjjJHqRPKO2jWI0V9vSmApFND8CqWms';

export const supabase = createClient(supabaseUrl, supabaseAnonKey, {
  auth: {
    storage: AsyncStorage,
    autoRefreshToken: true,
    persistSession: true,
    detectSessionInUrl: false,
  },
});
