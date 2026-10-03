'use client';

// Tabs switch between views of one page; Choice picks one or more options in
// a form. Both look the same everywhere.
export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: readonly (readonly [T, string])[]; value: T; onChange: (v: T) => void }) {
  return (
    <div className="flex gap-1 border-b border-border">
      {tabs.map(([id, label]) => (
        <button
          key={id}
          onClick={() => onChange(id)}
          className={`px-3 py-2 text-sm -mb-px border-b-2 transition-colors ${
            value === id ? 'border-accent text-text-primary' : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

export function Choice({ selected, onClick, children }: { selected: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`h-7 px-2.5 rounded-md text-xs border transition-colors ${
        selected ? 'border-accent bg-accent/10 text-text-primary' : 'border-border text-text-secondary hover:text-text-primary hover:border-border-strong'
      }`}
    >
      {children}
    </button>
  );
}
