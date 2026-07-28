import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [tailwindcss()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api/v1/admin": {
        target: "http://prism-api:8091",
        changeOrigin: true,
      },
    },
  },
});
