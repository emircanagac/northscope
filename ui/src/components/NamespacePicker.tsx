import { ChevronDown, X } from 'lucide-react';
import { useId, useMemo, useRef, useState } from 'react';
import { filterNamespaceOptions, plural } from '../inventory';
import { namespaceEscapeAction, namespaceInputValue, resolveNamespaceEnterSelection, type NamespaceOption } from '../uiState';
import { cx } from './ui';

interface PickerItem {
  value: string;
  label: string;
  detail: string;
  rooted: boolean;
}

// NamespacePicker is a WAI-ARIA combobox: type to filter, arrow keys to move
// through the list, Enter to pick, Escape to restore or clear.
export function NamespacePicker({
  namespace,
  options,
  hasSnapshot,
  onSelect,
}: {
  namespace: string;
  options: NamespaceOption[];
  hasSnapshot: boolean;
  onSelect: (namespace: string) => void;
}) {
  const id = useId();
  const listId = `${id}-list`;
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState(namespace);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);

  const filtered = useMemo(() => filterNamespaceOptions(options, query), [options, query]);
  const items = useMemo<PickerItem[]>(() => {
    const normalized = query.trim().toLowerCase();
    const all: PickerItem[] =
      !normalized || 'all namespaces'.includes(normalized) ? [{ value: '', label: 'All namespaces', detail: 'all', rooted: true }] : [];
    return all.concat(
      filtered.map((option) => ({
        value: option.name,
        label: option.name,
        detail: plural(option.ingressCount, 'root'),
        rooted: option.ingressCount > 0,
      })),
    );
  }, [filtered, query]);

  const close = () => {
    setOpen(false);
    setActive(-1);
  };
  const choose = (value: string) => {
    onSelect(value);
    setQuery(value);
    close();
  };
  const openList = () => {
    setOpen(true);
    setActive(-1);
  };

  return (
    <div className="relative">
      <label htmlFor={`${id}-input`} className="text-xs font-semibold text-fg">
        Namespace
      </label>
      <div className="relative mt-1">
        <input
          ref={inputRef}
          id={`${id}-input`}
          role="combobox"
          aria-expanded={open}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={open && active >= 0 ? `${id}-option-${active}` : undefined}
          autoComplete="off"
          spellCheck={false}
          value={namespaceInputValue(namespace, open, query)}
          placeholder={hasSnapshot ? 'All namespaces' : 'Waiting for namespaces'}
          onFocus={() => {
            setQuery(namespace);
            openList();
          }}
          onBlur={() => {
            window.setTimeout(() => {
              setQuery(namespace);
              close();
            }, 120);
          }}
          onChange={(event) => {
            const value = event.target.value;
            setQuery(value);
            if (!value.trim() && namespace) {
              onSelect('');
            }
            setOpen(true);
            setActive(-1);
          }}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault();
              if (!open) {
                setOpen(true);
              }
              const step = event.key === 'ArrowDown' ? 1 : -1;
              setActive((current) => (items.length === 0 ? -1 : (current + step + items.length) % items.length));
              return;
            }
            if (event.key === 'Home' && open) {
              event.preventDefault();
              setActive(items.length > 0 ? 0 : -1);
              return;
            }
            if (event.key === 'End' && open) {
              event.preventDefault();
              setActive(items.length - 1);
              return;
            }
            if (event.key === 'Enter') {
              event.preventDefault();
              if (open && active >= 0 && items[active]) {
                choose(items[active].value);
                return;
              }
              const selected = resolveNamespaceEnterSelection(query, filtered);
              if (selected !== null) {
                choose(selected);
              }
              return;
            }
            if (event.key === 'Escape') {
              if (namespaceEscapeAction(namespace, query, open) === 'clear') {
                choose('');
              } else {
                setQuery(namespace);
                close();
              }
            }
          }}
          className="h-8 w-full rounded-md border border-line bg-surface px-3 pr-14 text-sm text-fg shadow-card placeholder:text-fg-subtle focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent"
        />
        <div className="absolute inset-y-0 right-1 flex items-center gap-0.5">
          {namespace || query ? (
            <button
              type="button"
              aria-label="Clear namespace"
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => choose('')}
              className="grid h-6 w-6 place-items-center rounded text-fg-muted hover:bg-surface-hover hover:text-fg"
            >
              <X aria-hidden="true" className="h-3.5 w-3.5" />
            </button>
          ) : null}
          <ChevronDown aria-hidden="true" className="pointer-events-none mr-1 h-4 w-4 text-fg-muted" />
        </div>
        {open ? (
          <ul
            id={listId}
            role="listbox"
            aria-label="Namespaces"
            className="absolute left-0 right-0 top-9 z-30 max-h-72 overflow-auto rounded-md border border-line bg-surface py-1 shadow-overlay"
          >
            {items.length > 0 ? (
              items.map((item, index) => {
                const selected = item.value === namespace;
                return (
                  <li
                    key={item.value || '__all__'}
                    id={`${id}-option-${index}`}
                    role="option"
                    aria-selected={selected}
                    onMouseDown={(event) => event.preventDefault()}
                    onMouseEnter={() => setActive(index)}
                    onClick={() => choose(item.value)}
                    className={cx(
                      'flex cursor-pointer items-center justify-between gap-3 px-3 py-1.5 text-sm',
                      index === active ? 'bg-surface-hover' : '',
                      selected ? 'font-semibold text-fg' : 'text-fg',
                    )}
                  >
                    <span className="min-w-0 truncate">{item.label}</span>
                    <span className={cx('shrink-0 text-xs', item.rooted ? 'text-fg-muted' : 'text-fg-subtle')}>{item.detail}</span>
                  </li>
                );
              })
            ) : (
              <li className="px-3 py-1.5 text-sm text-fg-muted">No namespace matches</li>
            )}
          </ul>
        ) : null}
      </div>
    </div>
  );
}
