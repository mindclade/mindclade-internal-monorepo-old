export interface SafetyCheck {
    name: string;
    result: "pass" | "fail" | "pending";
    evidence?: string;
}
export declare function SafetyGate({ checks }: {
    checks: readonly SafetyCheck[];
}): React.ReactNode;
