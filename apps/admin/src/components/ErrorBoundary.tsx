// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { Button } from "@mindclade/libs-ts-design-system";
import { Component, type ErrorInfo, type ReactNode } from "react";

export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | undefined }> {
  state: { error: Error | undefined } = { error: undefined };
  static getDerivedStateFromError(error: Error): { error: Error } { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo): void { console.error("Admin render failure", error, info.componentStack); }
  render(): ReactNode { return this.state.error === undefined ? this.props.children : <section className="admin-panel admin-error"><span>Fail closed</span><h1>Administrative view interrupted</h1><p>{this.state.error.message}</p><Button onClick={() => this.setState({ error: undefined })}>Retry rendering</Button></section>; }
}
