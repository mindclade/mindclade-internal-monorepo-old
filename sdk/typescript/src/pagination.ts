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

export async function* paginate<T>(
  fetchPage: (options: PageOptions) => Promise<Page<T>>,
  options: PageOptions = {},
): AsyncGenerator<T, void, undefined> {
  let pageToken = options.pageToken;
  do {
    const page = await fetchPage({
      ...(options.pageSize === undefined ? {} : { pageSize: options.pageSize }),
      ...(pageToken === undefined ? {} : { pageToken }),
    });
    yield* page.items;
    pageToken = page.page.nextPageToken || undefined;
  } while (pageToken !== undefined);
}
