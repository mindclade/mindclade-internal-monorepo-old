export interface RolloutTarget {
    region: string;
    version: string;
    replicas: number;
    healthy: number;
}
export declare function RolloutFleet({ targets }: {
    targets: readonly RolloutTarget[];
}): React.ReactNode;
