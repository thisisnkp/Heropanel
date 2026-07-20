import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "./ui";

// A right-click menu, rendered in a portal at the pointer.
//
// It exists because a file manager whose actions live only in row buttons makes
// people hunt: the actions belong where the pointer already is. The menu is
// keyboard-operable for the same reason the rest of the panel is — arrows move,
// Enter selects, Esc closes — so nothing here is reachable by mouse alone.

export interface MenuItem {
  label: string;
  onSelect: () => void;
  /** Rendered right-aligned as a hint; purely decorative. */
  shortcut?: string;
  disabled?: boolean;
  danger?: boolean;
  /** Draws a divider above this item. */
  separatorBefore?: boolean;
}

/** Items are filtered before render, so callers can inline `false &&` entries. */
export type MenuItems = (MenuItem | false | null | undefined)[];

const MENU_WIDTH = 208; // w-52; needed up front to decide whether to flip

export function ContextMenu({
  x,
  y,
  items,
  onClose,
}: {
  x: number;
  y: number;
  items: MenuItems;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement | null>(null);
  const list = items.filter(Boolean) as MenuItem[];
  const [pos, setPos] = useState({ x, y });
  const [active, setActive] = useState(-1);

  // Flip the menu back into the viewport before the browser paints it, so it
  // never appears half off-screen near the bottom or right edge.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const h = el.offsetHeight;
    const nx = x + MENU_WIDTH > window.innerWidth ? Math.max(8, x - MENU_WIDTH) : x;
    const ny = y + h > window.innerHeight ? Math.max(8, y - h) : y;
    setPos({ x: nx, y: ny });
    el.focus();
  }, [x, y]);

  // Any interaction outside the menu dismisses it. Scroll and resize count:
  // the menu is anchored to a point, so once the page moves under it the
  // position is a lie.
  useEffect(() => {
    const close = () => onClose();
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    window.addEventListener("mousedown", onDown);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [onClose]);

  const move = (delta: number) => {
    if (list.length === 0) return;
    let i = active;
    for (let n = 0; n < list.length; n++) {
      i = (i + delta + list.length) % list.length;
      if (!list[i].disabled) break;
    }
    setActive(i);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    switch (e.key) {
      case "Escape":
        e.preventDefault();
        onClose();
        break;
      case "ArrowDown":
        e.preventDefault();
        move(1);
        break;
      case "ArrowUp":
        e.preventDefault();
        move(-1);
        break;
      case "Enter":
      case " ": {
        const item = list[active];
        if (!item || item.disabled) return;
        e.preventDefault();
        onClose();
        item.onSelect();
        break;
      }
    }
  };

  if (list.length === 0) return null;

  return createPortal(
    <div
      ref={ref}
      role="menu"
      tabIndex={-1}
      onKeyDown={onKeyDown}
      onContextMenu={(e) => e.preventDefault()}
      style={{ left: pos.x, top: pos.y }}
      className="fixed z-50 w-52 overflow-hidden rounded-lg border border-border bg-panel py-1 shadow-lg outline-none"
    >
      {list.map((item, i) => (
        <div key={item.label}>
          {item.separatorBefore && <div className="my-1 border-t border-border" />}
          <button
            role="menuitem"
            disabled={item.disabled}
            onMouseEnter={() => setActive(i)}
            onClick={() => {
              onClose();
              item.onSelect();
            }}
            className={cn(
              "flex w-full items-center justify-between gap-4 px-3 py-1.5 text-left text-sm",
              item.disabled
                ? "cursor-not-allowed text-muted/50"
                : item.danger
                  ? "text-danger hover:bg-danger/10"
                  : "text-fg hover:bg-border/60",
              i === active && !item.disabled && (item.danger ? "bg-danger/10" : "bg-border/60"),
            )}
          >
            <span className="truncate">{item.label}</span>
            {item.shortcut && <span className="shrink-0 font-mono text-[10px] text-muted">{item.shortcut}</span>}
          </button>
        </div>
      ))}
    </div>,
    document.body,
  );
}

// useContextMenu holds the "where was the pointer" state a right-click menu
// needs, plus the payload the menu should act on.
export function useContextMenu<T>() {
  const [state, setState] = useState<{ x: number; y: number; target: T } | null>(null);
  return {
    menu: state,
    close: () => setState(null),
    open: (e: React.MouseEvent, target: T) => {
      e.preventDefault();
      e.stopPropagation();
      setState({ x: e.clientX, y: e.clientY, target });
    },
  };
}
