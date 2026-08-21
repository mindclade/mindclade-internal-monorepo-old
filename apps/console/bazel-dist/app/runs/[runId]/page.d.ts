export default function Page({ params }: {
    params: Promise<{
        runId: string;
    }>;
}): Promise<React.ReactNode>;
