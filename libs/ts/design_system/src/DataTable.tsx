// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactElement, ReactNode } from "react";

export interface Column<T> {
  key: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  align?: "start" | "center" | "end";
}

export interface DataTableProps<T> {
  caption: string;
  columns: readonly Column<T>[];
  rows: readonly T[];
  rowKey: (row: T) => string;
  empty?: ReactNode;
}

export function DataTable<T>({ caption, columns, rows, rowKey, empty = "No records match this view." }: DataTableProps<T>): ReactElement {
  return (
    <div className="mc-table-wrap">
      <table className="mc-table">
        <caption className="mc-visually-hidden">{caption}</caption>
        <thead><tr>{columns.map((column) => <th key={column.key} scope="col" data-align={column.align ?? "start"}>{column.header}</th>)}</tr></thead>
        <tbody>
          {rows.length === 0 ? (
            <tr><td className="mc-table__empty" colSpan={columns.length}>{empty}</td></tr>
          ) : rows.map((row) => (
            <tr key={rowKey(row)}>{columns.map((column) => <td key={column.key} data-align={column.align ?? "start"}>{column.cell(row)}</td>)}</tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
