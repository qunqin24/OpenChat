/** Base URL for every API call; the Vite dev proxy forwards /api to the Go server. */
export const API_BASE_URL: string = import.meta.env.VITE_API_BASE_URL ?? '/api/v1'
