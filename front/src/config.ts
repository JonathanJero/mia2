// Configuración central para la URL del backend
// Permite sobrescribir la URL mediante la variable de entorno VITE_BACKEND_URL
const backendUrl = (import.meta as any)?.env?.VITE_BACKEND_URL ?? 'http://localhost:8080';
export const BACKEND_URL: string = backendUrl;
