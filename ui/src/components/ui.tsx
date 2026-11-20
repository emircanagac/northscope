import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { CircleCheck, CircleExclamationPoint, CircleDashed, TriangleExclamationPoint, type LucideIcon } from 'lucide-react';

export function cx(...classes: Array<string | false | null | undefined>): string {
  return classes.filter(Boolean).join(' ');
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  icon?: LucideIcon;
  variant?: 'default' | 'invisible';
};

// Button follows Primer's default and invisible buttons: a bordered neutral
// button for actions and a borderless one for low-emphasis controls.
export function Button({ icon: Icon, variant = 'default', className, children, type = 'button', ...props }: ButtonProps) {
  return (
    <button
      type={type}
      className={cx(
        'inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md px-3 text-[13px] font-medium text-fg transition-colors',
        variant === 'default'
          ? 'border border-line bg-surface shadow-card hover:bg-surface-hover'
          : 'text-fg-muted hover:bg-surface-hover hover:text-fg',
        className,
      )}
      {...props}
    >
      {Icon ? <Icon aria-hidden="true" className="h-4 w-4 shrink-0" strokeWidth={1.75} /> : null}
      {children}
    </button>
  );
}

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
}

export function SegmentedControl<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: Array<SegmentedOption<T>>;
  value: T;
  onChange: (value: T) => void;
}) {
  return (
    <div role="group" aria-label={label} className="inline-flex h-8 rounded-md border border-line bg-surface-muted p-0.5">
      {options.map((option) => {
        const selected = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={selected}
            onClick={() => onChange(option.value)}
            className={cx(
              'rounded px-3 text-[13px] transition-colors',
              selected ? 'border border-line bg-surface font-semibold text-fg shadow-card' : 'text-fg-muted hover:text-fg',
            )}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}

export type Tone = 'success' | 'attention' | 'danger' | 'neutral' | 'accent';

const toneText: Record<Tone, string> = {
  success: 'text-success',
  attention: 'text-attention',
  danger: 'text-danger',
  neutral: 'text-fg-muted',
  accent: 'text-accent',
};

const toneBorder: Record<Tone, string> = {
  success: 'border-success',
  attention: 'border-attention',
  danger: 'border-danger',
  neutral: 'border-line',
  accent: 'border-accent',
};

export function toneForSeverity(severity: string): Tone {
  switch (severity.toLowerCase()) {
    case 'error':
      return 'danger';
    case 'warning':
      return 'attention';
    case 'ok':
      return 'success';
    default:
      return 'neutral';
  }
}

const toneIcon: Record<Tone, LucideIcon> = {
  success: CircleCheck,
  attention: TriangleExclamationPoint,
  danger: CircleExclamationPoint,
  neutral: CircleDashed,
  accent: CircleDashed,
};

// StatusIcon shows health without relying on color alone: each tone has its
// own shape.
export function StatusIcon({ tone, className, label }: { tone: Tone; className?: string; label?: string }) {
  const Icon = toneIcon[tone];
  return (
    <Icon
      aria-hidden={label ? undefined : 'true'}
      aria-label={label}
      role={label ? 'img' : undefined}
      className={cx('h-4 w-4 shrink-0', toneText[tone], className)}
      strokeWidth={2}
    />
  );
}

// StateLabel is Primer's Label: a bordered pill whose color carries state.
export function StateLabel({ tone, children, title, className }: { tone: Tone; children: ReactNode; title?: string; className?: string }) {
  return (
    <span
      title={title}
      className={cx(
        'inline-flex max-w-full shrink-0 items-center truncate rounded-full border px-2 text-xs font-medium leading-5',
        toneBorder[tone],
        toneText[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

export function Counter({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span className={cx('inline-flex min-w-5 justify-center rounded-full bg-surface-hover px-1.5 text-xs font-medium leading-5 text-fg-muted', className)}>
      {children}
    </span>
  );
}
