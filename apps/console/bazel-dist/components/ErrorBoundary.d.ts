import { Component, type ErrorInfo, type ReactNode } from "react";
export declare class ErrorBoundary extends Component<{
    children: ReactNode;
}, {
    error: Error | undefined;
}> {
    state: {
        error: Error | undefined;
    };
    static getDerivedStateFromError(error: Error): {
        error: Error;
    };
    componentDidCatch(error: Error, info: ErrorInfo): void;
    render(): ReactNode;
}
