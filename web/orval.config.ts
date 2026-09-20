import { defineConfig } from "orval";

export default defineConfig({
  posCafe: {
    input: {
      target: "../docs/swagger.yaml",
    },
    output: {
      mode: "tags-split",
      target: "src/api/generated/endpoints",
      schemas: "src/api/generated/models",
      client: "react-query",
      httpClient: "axios",
      override: {
        mutator: {
          path: "src/lib/api-client.ts",
          name: "customAxiosInstance",
        },
        query: {
          useQuery: true,
          useMutation: true,
          options: {
            staleTime: 1000 * 30,
          },
        },
      },
    },
  },
});
