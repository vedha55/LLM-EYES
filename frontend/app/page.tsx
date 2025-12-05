import VideoRoom from "@/components/VideoRoom";

export default function Home() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center p-4">
      <h1 className="text-4xl font-bold mb-8">LLM-EYES Demo</h1>
      <VideoRoom />
    </main>
  );
}
