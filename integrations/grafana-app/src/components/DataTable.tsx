import { Icon, useStyles2 } from "@grafana/ui";
import { type ReactNode, useMemo, useState } from "react";

import { getTableStyles } from "../styles";

export interface Column<Row> {
  label: string;
  render: (row: Row) => ReactNode;
  sortValue?: (row: Row) => number;
  numeric?: boolean;
  className?: (row: Row) => string;
  title?: string;
}

export function DataTable<Row>({
  columns,
  rows,
  rowKey,
  sortedBy,
}: {
  columns: Array<Column<Row>>;
  rows: Row[];
  rowKey: (row: Row) => string;
  // Label of the column the rows already arrive sorted by, descending.
  sortedBy?: string;
}) {
  const styles = useStyles2(getTableStyles);
  const initialColumn = columns.findIndex((column) => column.label === sortedBy);
  const [sort, setSort] = useState<{ column: number; descending: boolean } | undefined>(
    initialColumn >= 0 ? { column: initialColumn, descending: true } : undefined,
  );

  const sortedRows = useMemo(() => {
    const sortValue = sort && columns[sort.column]?.sortValue;
    if (!sort || !sortValue) {
      return rows;
    }
    const direction = sort.descending ? -1 : 1;
    return [...rows].sort((left, right) => {
      const leftValue = sortValue(left);
      const rightValue = sortValue(right);
      if (Number.isNaN(leftValue)) {
        return 1;
      }
      if (Number.isNaN(rightValue)) {
        return -1;
      }
      return (leftValue - rightValue) * direction;
    });
  }, [rows, columns, sort]);

  const toggle = (column: number) =>
    setSort((previous) =>
      previous?.column === column
        ? { column, descending: !previous.descending }
        : { column, descending: true },
    );

  return (
    <table className={styles.table}>
      <thead>
        <tr>
          {columns.map((column, index) =>
            column.sortValue ? (
              <th
                key={column.label}
                className={`${column.numeric ? "number" : ""} ${styles.sortable}`}
                title={column.title}
                onClick={() => toggle(index)}
              >
                {column.label}
                {sort?.column === index && (
                  <Icon name={sort.descending ? "arrow-down" : "arrow-up"} size="sm" />
                )}
              </th>
            ) : (
              <th
                key={column.label}
                className={column.numeric ? "number" : ""}
                title={column.title}
              >
                {column.label}
              </th>
            ),
          )}
        </tr>
      </thead>
      <tbody>
        {sortedRows.map((row) => (
          <tr key={rowKey(row)}>
            {columns.map((column) => (
              <td
                key={column.label}
                className={`${column.numeric ? "number" : ""} ${column.className?.(row) ?? ""}`}
              >
                {column.render(row)}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
