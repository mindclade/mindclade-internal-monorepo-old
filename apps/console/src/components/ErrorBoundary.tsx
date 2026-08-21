// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { Button } from "@mindclade/libs-ts-design-system";
import { Component, type ErrorInfo, type ReactNode } from "react";
import { captureConsoleEvent } from "../lib/events";

export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | undefined }> {
  state: { error: Error | undefined } = { error: undefined };

  static getDerivedStateFromError(error: Error): { error: Error } { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo): void {
    captureConsoleEvent("ui.render_error", { surface: "console" });
    if (process.env.NODE_ENV !== "production") console.error("Console render failure", error, info.componentStack);
  }

  render(): ReactNode {
    if (this.state.error === undefined) return this.props.children;
    return <section className="state-message state-message--error"><span>Interface interrupted</span><h1>This view couldn’t be rendered.</h1><p>No action was taken. Try rendering the view again; contact support if the interruption persists.</p><Button onClick={() => this.setState({ error: undefined })}>Try again</Button></section>;
  }
}
