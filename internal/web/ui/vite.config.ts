import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],

  build: {
    // Output goes to dist/ inside internal/web/ui/ so go:embed can pick it up.
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ["react", "react-dom", "react-router-dom"],
          connect: [
            "@connectrpc/connect",
            "@connectrpc/connect-web",
            "@bufbuild/protobuf",
          ],
          md: ["react-markdown", "rehype-highlight", "remark-gfm"],
        },
      },
    },
  },

  // Dev server: proxy Connect RPC calls to the running Go server.
  server: {
    port: 5173,
    proxy: {
      "/feino.v1.FeinoService": {
        target: "http://localhost:3000",
        changeOrigin: true,
      },
    },
  },
});
