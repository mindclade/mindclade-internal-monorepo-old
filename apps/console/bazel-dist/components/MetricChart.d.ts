import { type LinePoint } from "@mindclade/libs-ts-charts";
export declare function MetricChart({ label, points, value }: {
    label: string;
    points: readonly LinePoint[];
    value?: string;
}): React.ReactNode;
