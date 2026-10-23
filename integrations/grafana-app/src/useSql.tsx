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

export function useSql(sql: string | undefined): SqlState {
  const [state, setState] = useState<SqlState>(() => {
    const rows = cached(sql);
    return rows ? { rows, loading: false } : { rows: [], loading: !!sql };
  });
  useEffect(() => {
    if (!sql) {
      setState({ rows: [], loading: false });
      return;
    }
    const rows = cached(sql);
    if (rows) {
      setState({ rows, loading: false });
      return;
    }
    let cancelled = false;
    setState((previous) => ({ ...previous, loading: true, error: undefined }));
    runSql(sql).then(
      (fetched) => {
        cache.set(sql, { rows: fetched, fetchedAt: Date.now() });
        if (!cancelled) {
          setState({ rows: fetched, loading: false });
        }
      },
      (error: { message?: string; data?: { message?: string } }) =>
        !cancelled &&
        setState({
          rows: [],
          loading: false,
          error: error.data?.message ?? error.message ?? String(error),
        }),
    );
    return () => {
      cancelled = true;
    };
  }, [sql]);
  return state;
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
