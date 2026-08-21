import type { MindcladeClient } from "@mindclade/sdk-typescript";
import type { PublicResourceKind, ResourceRow } from "./types";
export declare function apiClient(): MindcladeClient;
export declare function loadResources(kind: PublicResourceKind, signal: AbortSignal): Promise<ResourceRow[]>;
