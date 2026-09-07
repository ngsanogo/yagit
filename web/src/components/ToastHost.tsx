import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';

import { Toast, type ToastTone } from './Toast';

interface ToastMessage {
  id: number;
  tone: ToastTone;
  title: string;
  detail?: ReactNode;
}

interface ToastHostContextValue {
  push: (message: Omit<ToastMessage, 'id'>) => void;
}

const ToastHostContext = createContext<ToastHostContextValue | null>(null);

/**
 * How long a toast nobody has to act on stays on screen.
 *
 * Long enough to read a short sentence twice, which is what "Committed" is.
 * A danger toast is exempt: it carries the raw stderr of a command that
 * failed, and taking that away on a timer would be the interface deciding the
 * user had finished reading their only copy of it.
 */
const TRANSIENT_MS = 4_000;

/**
 * A small stack of notifications, fixed to the bottom corner.
 *
 * Mutations that fail outside the form they came from — closing a tab, for
 * example — have nowhere else to put their error. A toast is honest about
 * being transient: the user can dismiss it and keep working.
 *
 * The BOTTOM corner, and that is the whole reason this note exists. The
 * header's actions live in the top right, so a stack anchored there covers
 * the controls the user reaches for next — and a danger toast, which never
 * expires, covered them for good.
 *
 * Anchored at the bottom, the box grows upwards and the last child is the one
 * in the corner. So the list is rendered in the order it was pushed: the
 * newest toast always appears in the same place, and the older ones move out
 * of its way.
 */
export function ToastHost({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  // A ref, not state. Two toasts pushed before React re-renders — a close that
  // fails on two tabs at once, which is exactly when this component earns its
  // place — would both read the same value out of state and come out with the
  // same key. React then draws one of them, and dismissing it dismisses the
  // other. A counter that is not rendered has no business being state, and
  // keeping it out of the dependency list is what makes push stable, so no
  // consumer re-renders because a toast appeared somewhere else.
  const nextId = useRef(0);

  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id));
  }, []);

  const push = useCallback((message: Omit<ToastMessage, 'id'>) => {
    const id = nextId.current;
    nextId.current += 1;
    setToasts((current) => [...current, { ...message, id }]);
  }, []);

  const value = useMemo(() => ({ push }), [push]);

  return (
    <ToastHostContext.Provider value={value}>
      {children}
      <div
        aria-live="polite"
        className="pointer-events-none fixed right-3 bottom-3 z-[100] flex flex-col gap-2"
      >
        {toasts.map((toast) => (
          <div key={toast.id} className="pointer-events-auto">
            <Toast
              tone={toast.tone}
              title={toast.title}
              detail={toast.detail}
              onDismiss={() => dismiss(toast.id)}
            />
            {toast.tone !== 'danger' && <Expiry id={toast.id} onExpire={dismiss} />}
          </div>
        ))}
      </div>
    </ToastHostContext.Provider>
  );
}

/**
 * The timer for one toast, as a component rather than an effect in the host.
 *
 * Mounted with the toast and unmounted with it, so the timeout is cleared by
 * React's own lifecycle. Held in the host instead, it would be a map of
 * identifiers to handles that has to be kept in step with the list by hand —
 * and the failure mode of getting that wrong is a toast that dismisses the one
 * that replaced it.
 */
function Expiry({ id, onExpire }: { id: number; onExpire: (id: number) => void }) {
  useEffect(() => {
    const handle = window.setTimeout(() => onExpire(id), TRANSIENT_MS);
    return () => window.clearTimeout(handle);
  }, [id, onExpire]);

  return null;
}

export function useToast(): ToastHostContextValue {
  const context = useContext(ToastHostContext);
  if (context === null) {
    throw new Error('useToast must be used inside ToastHost');
  }
  return context;
}
