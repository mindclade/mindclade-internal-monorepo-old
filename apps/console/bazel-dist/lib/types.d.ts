export type PublicResourceKind = "runs" | "datasets" | "models" | "artifacts" | "evaluations";
export interface ResourceRow {
    id: string;
    name: string;
    kind: string;
    status: string;
    updatedAt: string;
    href?: string;
}
export interface ResourcePageCopy {
    eyebrow: string;
    title: string;
    description: string;
    emptyTitle: string;
    emptyDetail: string;
    action?: string;
}
