import AsyncStorage from '@react-native-async-storage/async-storage';

export interface Snippet {
  id: string;
  title: string;
  command: string;
}

const STORAGE_KEY = '@pokit_snippets';

export async function getSnippets(): Promise<Snippet[]> {
  try {
    const jsonValue = await AsyncStorage.getItem(STORAGE_KEY);
    return jsonValue != null ? JSON.parse(jsonValue) : [];
  } catch (e) {
    console.error('Failed to get snippets', e);
    return [];
  }
}

export async function saveSnippet(snippet: Snippet): Promise<Snippet[]> {
  try {
    const currentSnippets = await getSnippets();
    const newSnippets = [...currentSnippets, snippet];
    await AsyncStorage.setItem(STORAGE_KEY, JSON.stringify(newSnippets));
    return newSnippets;
  } catch (e) {
    console.error('Failed to save snippet', e);
    return [];
  }
}

export async function deleteSnippet(id: string): Promise<Snippet[]> {
  try {
    const currentSnippets = await getSnippets();
    const newSnippets = currentSnippets.filter(s => s.id !== id);
    await AsyncStorage.setItem(STORAGE_KEY, JSON.stringify(newSnippets));
    return newSnippets;
  } catch (e) {
    console.error('Failed to delete snippet', e);
    return [];
  }
}
