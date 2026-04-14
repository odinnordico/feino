// Shared react-markdown configuration used by MarkdownRenderer.
// Plugins and component overrides are defined here so they are not
// recreated per render.

import remarkGfm          from "remark-gfm";
import rehypeHighlight    from "rehype-highlight";
import "./highlight"; // side-effect: register languages

export const remarkPlugins = [remarkGfm];
export const rehypePlugins = [rehypeHighlight];
