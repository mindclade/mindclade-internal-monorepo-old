export interface KernelCheck {
    name: string;
    platform: string;
    status: "qualified" | "testing" | "rejected";
    variance?: number;
}
export declare function KernelQualification({ checks }: {
    checks: readonly KernelCheck[];
}): React.ReactNode;
