export interface RunnerDef {
  id: string;
  name: string;
  icon: string;
}

export const RUNNERS: RunnerDef[] = [
  { id: 'cat', name: 'Cat', icon: 'cat' },
  { id: 'parrot', name: 'Parrot', icon: 'crow' }, 
  { id: 'dog', name: 'Dog', icon: 'dog' },
  { id: 'slime', name: 'Slime', icon: 'ghost' },
  { id: 'mario', name: 'Mario', icon: 'gamepad' },
  { id: 'sonic', name: 'Sonic', icon: 'fighter-jet' },
  { id: 'nyancat', name: 'Nyan Cat', icon: 'space-shuttle' },
  { id: 'pacman', name: 'Pac-Man', icon: 'bug' },
  { id: 'gear', name: 'Gear', icon: 'cog' },
  { id: 'bonfire', name: 'Bonfire', icon: 'fire' },
];

export const PRESET_COLORS = [
  '#58a6ff', // Blue
  '#238636', // Green
  '#e3b341', // Yellow
  '#f85149', // Red
  '#a371f7', // Purple
  '#d29922', // Orange
  '#39d353', // Light Green
  '#ff7b72', // Pink
];
