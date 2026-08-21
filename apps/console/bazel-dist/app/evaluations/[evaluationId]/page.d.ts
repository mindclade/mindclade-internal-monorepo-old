export default function Page({ params }: {
    params: Promise<{
        evaluationId: string;
    }>;
}): Promise<React.ReactNode>;
