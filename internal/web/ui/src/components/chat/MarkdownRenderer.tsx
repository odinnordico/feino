import ReactMarkdown from "react-markdown";
import { remarkPlugins, rehypePlugins } from "../../lib/markdown";

type MarkdownRendererProps = {
  content: string;
}

export function MarkdownRenderer({ content }: MarkdownRendererProps) {
  return (
    <div className="prose-neural">
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={rehypePlugins}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
