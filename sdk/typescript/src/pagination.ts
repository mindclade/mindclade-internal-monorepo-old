// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface PageOptions {
  pageSize?: number;
  pageToken?: string;
}

export interface ListOptions extends PageOptions {
  signal?: AbortSignal;
}

export interface Page<T> {
  items: T[];
  page: { nextPageToken: string };
}

export interface PaginationOptions extends PageOptions {
  maxPages?: number;
  signal?: AbortSignal;
}

export async function* paginate<T>(
  fetchPage: (options: PageOptions) => Promise<Page<T>>,
  options: PaginationOptions = {},
): AsyncGenerator<T, void, undefined> {
  const maxPages = options.maxPages ?? 10_000;
  if (!Number.isSafeInteger(maxPages) || maxPages <= 0) throw new RangeError("maxPages must be a positive safe integer");
  let pageToken = options.pageToken;
  const seenTokens = new Set<string>();
  let pages = 0;
  do {
    if (options.signal?.aborted) throw options.signal.reason ?? new DOMException("Pagination cancelled", "AbortError");
    if (pages >= maxPages) throw new RangeError(`Pagination exceeded the ${maxPages} page limit`);
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new Error("Pagination returned a repeated page token");
      seenTokens.add(pageToken);
    }
    const page = await fetchPage({
      ...(options.pageSize === undefined ? {} : { pageSize: options.pageSize }),
      ...(pageToken === undefined ? {} : { pageToken }),
    });
    pages += 1;
    yield* page.items;
    pageToken = page.page.nextPageToken || undefined;
  } while (pageToken !== undefined);
}
