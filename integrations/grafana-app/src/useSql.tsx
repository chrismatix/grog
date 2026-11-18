import { Alert, LoadingPlaceholder } from "@grafana/ui";
import { useEffect, useState } from "react";

import { type Row, runSql } from "./sql/client";

export interface SqlState {
  rows: Row[];
  loading: boolean;
  error?: string;
}

// Results are reused for a few minutes so switching tabs back and forth is instant.
const cacheLifetimeMs = 5 * 60_000;
const cache = new Map<string, { rows: Row[]; fetchedAt: number }>();

function cached(sql: string | undefined): Row[] | undefined {
  const entry = sql ? cache.get(sql) : undefined;
  return entry && Date.now() - entry.fetchedAt < cacheLifetimeMs ? entry.rows : undefined;
}

function initialState(sql: string | undefined): SqlState & { sql?: string } {
  const rows = cached(sql);
  return rows ? { sql, rows, loading: false } : { sql, rows: [], loading: !!sql };
}

export function useSql(sql: string | undefined): SqlState {
  const [state, setState] = useState(() => initialState(sql));
  useEffect(() => {
    setState(initialState(sql));
    if (!sql || cached(sql)) {
      return;
    }
    let cancelled = false;
    runSql(sql).then(
      (fetched) => {
        cache.set(sql, { rows: fetched, fetchedAt: Date.now() });
        if (!cancelled) {
          setState({ sql, rows: fetched, loading: false });
        }
      },
      (error: Error) =>
        !cancelled && setState({ sql, rows: [], loading: false, error: error.message }),
    );
    return () => {
      cancelled = true;
    };
  }, [sql]);
  // Until the effect catches up, never hand out rows that belong to the previous query.
  return state.sql === sql ? state : initialState(sql);
}

export function SqlStatus({ state }: { state: SqlState }) {
  if (state.error) {
    return (
      <Alert title="Query failed" severity="error">
        {state.error}
      </Alert>
    );
  }
  if (state.loading) {
    return <LoadingPlaceholder text="Loading…" />;
  }
  return null;
}
