import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  Bell,
  Camera,
  Car,
  Loader2,
  MapPin,
  Search,
  Server,
  X,
} from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { Schemas } from "@/api/client";
import { searchQuery } from "@/api/queries";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";

type SearchResult = Schemas["SearchResult"];


export interface OmniboxProps {
  isOpen: boolean;
  onClose: () => void;
}

type FlatItem =
  | {
      category: "cameras";
      id: string;
      title: string;
      subtitle: string;
      status?: string;
      to: string;
      search?: Record<string, unknown>;
    }
  | {
      category: "sites";
      id: string;
      title: string;
      subtitle: string;
      to: string;
      search?: Record<string, unknown>;
    }
  | {
      category: "servers";
      id: string;
      title: string;
      subtitle: string;
      status?: string;
      to: string;
      search?: Record<string, unknown>;
    }
  | {
      category: "events";
      id: string;
      title: string;
      subtitle: string;
      severity?: string;
      to: string;
      search?: Record<string, unknown>;
    }
  | {
      category: "plates";
      id: string;
      title: string;
      subtitle: string;
      to: string;
      search?: Record<string, unknown>;
    };

const categoryConfig = {
  cameras: { title: "Cámaras", icon: Camera },
  sites: { title: "Sitios", icon: MapPin },
  servers: { title: "Servidores", icon: Server },
  events: { title: "Eventos", icon: Bell },
  plates: { title: "Patentes", icon: Car },
} as const;

export function Omnibox({ isOpen, onClose }: OmniboxProps) {
  const [q, setQ] = useState("");
  const [debouncedQ, setDebouncedQ] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listboxId = useId();
  const navigate = useNavigate();

  // Focus on open
  useEffect(() => {
    if (isOpen) {
      inputRef.current?.focus();
    }
  }, [isOpen]);

  // Debounce query (200ms)
  useEffect(() => {
    const handler = setTimeout(() => {
      setDebouncedQ(q.trim());
    }, 200);
    return () => clearTimeout(handler);
  }, [q]);

  const searchResults = useQuery(searchQuery(debouncedQ));

  const flatItems: FlatItem[] = useMemo(() => {
    if (!searchResults.data) return [];
    const data: SearchResult = searchResults.data;
    const items: FlatItem[] = [];

    if (data.cameras?.length) {
      for (const c of data.cameras) {
        items.push({
          category: "cameras",
          id: c.id,
          title: c.display_name,
          subtitle: c.location ? `${c.site_name} • ${c.location}` : c.site_name,
          status: c.status,
          to: "/cameras",
          search: { q: c.display_name },
        });
      }
    }

    if (data.sites?.length) {
      for (const s of data.sites) {
        items.push({
          category: "sites",
          id: s.id,
          title: s.name,
          subtitle: `${s.camera_count} cámara${s.camera_count === 1 ? "" : "s"}`,
          to: "/cameras",
          search: { site_id: s.id },
        });
      }
    }

    if (data.servers?.length) {
      for (const s of data.servers) {
        items.push({
          category: "servers",
          id: s.id,
          title: s.name,
          subtitle: s.site_name,
          status: s.status,
          to: "/settings/servers",
        });
      }
    }

    if (data.events?.length) {
      for (const e of data.events) {
        const labels = e.labels?.length ? e.labels.join(", ") : "Evento";
        items.push({
          category: "events",
          id: e.id,
          title: `${labels} (${e.camera_name})`,
          subtitle: `${e.site_name} • ${fmtDateTime(e.start_time)}`,
          severity: e.severity,
          to: "/events",
          search: { camera_id: [e.camera_id] },
        });
      }
    }

    if (data.plates?.length) {
      for (const p of data.plates) {
        items.push({
          category: "plates",
          id: `${p.camera_id}-${p.plate}-${p.seen_at}`,
          title: p.plate,
          subtitle: `${p.camera_name} • ${fmtDateTime(p.seen_at)}`,
          to: "/plates",
          search: { plate: p.plate },
        });
      }
    }

    return items;
  }, [searchResults.data]);

  const activeIndex = selectedIndex < flatItems.length ? selectedIndex : 0;

  const selectItem = (item: FlatItem) => {
    onClose();
    void navigate({
      to: item.to,
      search: item.search,
    } as unknown as Parameters<typeof navigate>[0]);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (flatItems.length > 0) {
        setSelectedIndex((prev) => (prev + 1) % flatItems.length);
      }
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      if (flatItems.length > 0) {
        setSelectedIndex((prev) => (prev - 1 + flatItems.length) % flatItems.length);
      }
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      if (flatItems.length > 0 && flatItems[activeIndex]) {
        selectItem(flatItems[activeIndex]);
      }
    }
  };

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-4 pt-16 backdrop-blur-xs md:pt-24"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Búsqueda global"
        className="flex max-h-[80vh] w-full max-w-2xl flex-col rounded-xl border border-line bg-surface shadow-2xl overflow-hidden"
      >
        {/* Top input bar */}
        <div className="flex items-center gap-3 border-b border-line px-4 py-3 bg-surface">
          <Search className="size-5 shrink-0 text-muted" aria-hidden />
          <input
            ref={inputRef}
            type="text"
            role="combobox"
            aria-expanded={flatItems.length > 0}
            aria-controls={listboxId}
            aria-activedescendant={
              flatItems[activeIndex] ? `omnibox-item-${activeIndex}` : undefined
            }
            aria-label="Buscar en OpenVMS"
            placeholder="Buscar cámaras, sitios, servidores, eventos, patentes..."
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={handleKeyDown}
            className="flex-1 bg-transparent text-sm text-ink outline-none placeholder:text-muted"
          />
          {q && (
            <button
              type="button"
              onClick={() => {
                setQ("");
                setDebouncedQ("");
                inputRef.current?.focus();
              }}
              aria-label="Limpiar búsqueda"
              className="rounded p-1 text-muted hover:bg-raised hover:text-ink"
            >
              <X className="size-4" aria-hidden />
            </button>
          )}
          <kbd className="hidden sm:inline-block rounded border border-line bg-bg px-1.5 py-0.5 font-mono text-[10px] text-muted">
            ESC
          </kbd>
        </div>

        {/* Results area */}
        <div
          id={listboxId}
          role="listbox"
          aria-label="Resultados de búsqueda"
          className="flex-1 overflow-y-auto p-2"
        >
          {debouncedQ.length < 2 && (
            <div className="px-4 py-8 text-center text-sm text-muted">
              Escribí al menos 2 caracteres para buscar en OpenVMS...
            </div>
          )}

          {debouncedQ.length >= 2 && searchResults.isPending && (
            <div className="flex items-center justify-center gap-2 px-4 py-8 text-sm text-muted">
              <Loader2 className="size-4 animate-spin" aria-hidden />
              <span>Buscando...</span>
            </div>
          )}

          {debouncedQ.length >= 2 && searchResults.isError && (
            <div className="px-4 py-8 text-center text-sm text-bad">
              Error al buscar: {searchResults.error.message}
            </div>
          )}

          {debouncedQ.length >= 2 &&
            !searchResults.isPending &&
            !searchResults.isError &&
            flatItems.length === 0 && (
              <div className="px-4 py-8 text-center text-sm text-muted">
                No se encontraron resultados para «<span className="text-ink font-medium">{debouncedQ}</span>».
              </div>
            )}

          {flatItems.length > 0 && (
            <div className="flex flex-col gap-1">
              {(
                [
                  "cameras",
                  "sites",
                  "servers",
                  "events",
                  "plates",
                ] as const
              ).map((cat) => {
                const catItems = flatItems
                  .map((item, idx) => ({ item, idx }))
                  .filter(({ item }) => item.category === cat);

                if (catItems.length === 0) return null;
                const { title, icon: CatIcon } = categoryConfig[cat];

                return (
                  <div key={cat} className="flex flex-col">
                    <div className="flex items-center gap-1.5 px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted">
                      <CatIcon className="size-3.5" aria-hidden />
                      <span>{title}</span>
                    </div>
                    {catItems.map(({ item, idx }) => {
                      const isSelected = idx === activeIndex;
                      return (
                        <div
                          key={item.id}
                          id={`omnibox-item-${idx}`}
                          role="option"
                          aria-selected={isSelected}
                          onMouseEnter={() => setSelectedIndex(idx)}
                          onClick={() => selectItem(item)}
                          className={cn(
                            "flex cursor-pointer items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors",
                            isSelected
                              ? "bg-accent/15 text-accent ring-1 ring-inset ring-accent/30"
                              : "hover:bg-raised text-ink",
                          )}
                        >
                          <div className="flex min-w-0 flex-col">
                            <span
                              className={cn(
                                "truncate font-medium",
                                item.category === "plates" && "font-mono font-bold tracking-wider",
                              )}
                            >
                              {item.title}
                            </span>
                            <span className="truncate text-xs text-muted">
                              {item.subtitle}
                            </span>
                          </div>

                          <div className="flex items-center gap-2 shrink-0">
                            {item.category === "cameras" && item.status && (
                              <span
                                className={cn(
                                  "rounded px-1.5 py-0.5 text-[10px] font-medium border",
                                  item.status === "online" || item.status === "ok"
                                    ? "bg-ok/10 border-ok/30 text-ok"
                                    : "bg-bad/10 border-bad/30 text-bad",
                                )}
                              >
                                {item.status}
                              </span>
                            )}
                            {item.category === "servers" && item.status && (
                              <span
                                className={cn(
                                  "rounded px-1.5 py-0.5 text-[10px] font-medium border",
                                  item.status === "ok"
                                    ? "bg-ok/10 border-ok/30 text-ok"
                                    : "bg-warn/10 border-warn/30 text-warn",
                                )}
                              >
                                {item.status}
                              </span>
                            )}
                            {item.category === "events" && item.severity && (
                              <span
                                className={cn(
                                  "rounded px-1.5 py-0.5 text-[10px] font-medium border",
                                  item.severity === "alert"
                                    ? "bg-bad/10 border-bad/30 text-bad"
                                    : "bg-warn/10 border-warn/30 text-warn",
                                )}
                              >
                                {item.severity}
                              </span>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between border-t border-line bg-raised/50 px-4 py-2 text-[11px] text-muted">
          <div className="flex items-center gap-3">
            <span>
              <kbd className="rounded border border-line bg-bg px-1 py-0.5 font-mono text-[10px]">
                ↑
              </kbd>{" "}
              <kbd className="rounded border border-line bg-bg px-1 py-0.5 font-mono text-[10px]">
                ↓
              </kbd>{" "}
              Navegar
            </span>
            <span>
              <kbd className="rounded border border-line bg-bg px-1 py-0.5 font-mono text-[10px]">
                ↵
              </kbd>{" "}
              Seleccionar
            </span>
            <span>
              <kbd className="rounded border border-line bg-bg px-1 py-0.5 font-mono text-[10px]">
                esc
              </kbd>{" "}
              Cerrar
            </span>
          </div>
          <span>Búsqueda global OpenVMS</span>
        </div>
      </div>
    </div>
  );
}
