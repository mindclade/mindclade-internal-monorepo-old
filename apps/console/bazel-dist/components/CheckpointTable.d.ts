export interface CheckpointRow {
    digest: string;
    step: number;
    sizeBytes: number;
    createdAt: string;
    verified: boolean;
}
export declare function CheckpointTable({ checkpoints }: {
    checkpoints: readonly CheckpointRow[];
}): React.ReactNode;
