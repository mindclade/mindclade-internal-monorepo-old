export interface TimelineStage {
    name: string;
    status: "waiting" | "running" | "complete" | "failed";
    detail?: string;
}
export declare function PreprocessingTimeline({ stages }: {
    stages: readonly TimelineStage[];
}): React.ReactNode;
