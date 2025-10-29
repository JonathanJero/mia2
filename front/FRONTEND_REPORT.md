# Reporte del frontend — ExtreamFS (React + Vite + TypeScript)

Este documento describe el frontend del proyecto ExtreamFS: cómo está construido, cómo se integra con el backend, los componentes principales y recomendaciones para uso e integración.

## Resumen

- Framework: React con TypeScript, empaquetado/servicio con Vite.
- Estilo: CSS simple (archivo `App.css`).
- Dependencias: React 19, Tailwind (mencionado en `package.json` aunque no vemos uso explícito en componentes), Vite para desarrollo y build.
- Scripts útiles (desde `front`):
  - `npm run dev` — levanta Vite en modo desarrollo.
  - `npm run build` — compila la app para producción.
  - `npm run preview` — previsualiza el build.

## Archivos de entrada

- `index.html` — contenedor HTML que carga `src/main.tsx`.
- `src/main.tsx` — renderiza `<App />` en `#root`.
- `src/App.tsx` — componente principal que contiene el editor de comandos, panel de salida y modales para login / visualización de FS / journaling.

## Cómo ejecutar (local)

1. Ir a la carpeta `front`:

```bash
cd front
```

2. Instalar dependencias:

```bash
npm install
```

3. Ejecutar en modo desarrollo:

```bash
npm run dev
```

Esto inicia el servidor Vite (por defecto en http://localhost:5173). El frontend consumirá un backend cuya URL está codificada en los componentes (ver sección de configuración).

## URL del backend

Actualmente todos los componentes usan la misma URL base codificada:

```ts
const BACKEND_URL = 'http://ec2-3-137-193-20.us-east-2.compute.amazonaws.com:8080';
```

Recomendación: mover esta URL a una variable de entorno (`import.meta.env.VITE_BACKEND_URL`) para facilitar despliegue y pruebas locales.

## Flujo de uso / UX principal

- En la pantalla principal (`App.tsx`) el usuario puede:
  - Escribir comandos en el editor (textarea) o cargar un archivo `.smia` desde el sistema.
  - Ejecutar todos los comandos mediante el botón "Ejecutar". Cada comando se envía al endpoint `/execute` en `BACKEND_URL`.
  - Ver la salida en el panel derecho.
  - Abrir el modal de "Iniciar Sesión" (Login) que pide seleccionar una partición montada y credenciales.
  - Abrir el "Visualizador" del sistema de archivos (FileSystemViewer) y el "Journaling Viewer".

- El editor respeta comentarios y muestra números de línea. Antes de ejecutar, el frontend verifica si hay comandos que requieren autenticación y, si no hay sesión, abre el modal de login.

## Principales componentes (resumen)

1. `App.tsx`
   - Estado principal: `commands` (texto), `output` (lista de resultados), `session` (usuario/partición/rol), `isConnected` a backend.
   - Funciones clave: `executeCommands()` (envía POST a `/execute` para cada comando), `checkBackendConnection()` (GET `/health`), manejo de sesión (`handleLogin`, `handleLogout`).
   - Modales: `Login`, `FileSystemViewer`, `JournalingViewer`.

2. `Login.tsx`
   - Carga particiones montadas desde `/disks/mounted`.
   - Autoselecciona la primera partición montada cuando existe.
   - Al enviar, construye el comando `login -user=... -pass=... -id=...` y lo envía a `/execute`.
   - Llama `onLogin` con `partitionId`, `username`, y boolean `isRoot` (basado en `username === 'root'`).

3. `FileSystemViewer.tsx`
   - Composición de `DiskSelector`, `PartitionViewer` y `FileExplorer`.
   - Carga discos con GET `/disks`.
   - Permite seleccionar disco → partición → explorar archivos.
   - Control de acceso: si el usuario es root o la partición coincide con la sesión, la exploración está permitida.

4. `DiskSelector.tsx`
   - Muestra tarjetas para cada disco (ruta, tamaño, fit, número de particiones). Llama `onSelectDisk` cuando se elige uno.

5. `PartitionViewer.tsx`
   - Muestra información de un disco y sus particiones.
   - Permite seleccionar una partición (si la sesión tiene acceso) y luego abrir `FileExplorer`.

6. `FileExplorer.tsx`
   - Interactúa con `/files` (POST con { partitionId, path }) para listar archivos en una ruta.
   - Interactúa con `/file/read` (POST con { partitionId, path }) para leer el contenido de un archivo.
   - Muestra modal con contenido, metadatos y permite navegar entre carpetas.

7. `JournalingViewer.tsx`
   - Interactúa con `/journaling` (POST con { partitionId }) para obtener las entradas del journal (EXT3).
   - Formatea timestamps y muestra una línea de tiempo con filtrado.

8. `FileExplorer` y `PartitionViewer` usan estructuras de tipo implícito (no centralizadas en un archivo `types.ts`).

## Endpoints usados (resumen y payloads)

- GET /health
  - Uso: verificación de conexión del frontend.
  - Respuesta esperada: 200 con JSON { status: 'ok', message: '...', version: '2.0' }.

- POST /execute
  - Body: { "command": "<comando CLI completo>" }
  - Respuesta: { success: boolean, output: string, error?: string }
  - Uso: ejecutar cualquier comando del sistema (mkdisk, fdisk, mkfs, login, mkfile, rep, logout, etc.)

- GET /disks
  - Uso: obtener lista completa de discos y particiones (información para `FileSystemViewer` y `DiskSelector`).
  - Respuesta: { disks: [...], count: n }

- GET /disks/mounted
  - Uso: obtener solo discos con particiones montadas (para auto-selección en `Login` y listas rápidas).
  - Respuesta: { disks: [...], count: n }

- POST /files
  - Body: { partitionId: string, path: string }
  - Uso: listar contenido de una ruta dentro de una partición montada.
  - Respuesta: { success: boolean, files: FileNode[], path: string, count: n }

- POST /file/read
  - Body: { partitionId: string, path: string }
  - Uso: leer contenido de archivo; requiere sesión activa en backend.
  - Respuesta: { success: boolean, content: string }

- POST /journaling
  - Body: { partitionId: string }
  - Uso: obtener entradas de journaling para EXT3.
  - Respuesta: { success: boolean, entries: [...], count: n }

## Observaciones técnicas detectadas

1. BACKEND_URL está hardcodeada en múltiples componentes. Mejor usar variable de entorno (`Vite` permite `VITE_*`).
2. No hay manejo centralizado de la sesión salvo el estado local de `App.tsx` — el backend mantiene su propia sesión en memoria. Si se recarga la página, la sesión del frontend se pierde (aunque la del backend puede persistir si no se reinicia). Se podría sincronizar con un endpoint `/session` para recuperar estado.
3. No hay debounce o limitador para peticiones; por ejemplo `/files` se llama cada vez que `currentPath` cambia — esto está bien, pero podría agregarse caching y manejo de errores más detallado.
4. Estructuras de datos (FileNode, Disk, Partition) están definidas localmente en varios archivos; sería mejor centralizarlas en `src/types.ts`.
5. Los componentes añaden logs a consola (especialmente `JournalingViewer`) — útil para debugging pero opcional en producción.
6. El frontend asume respuestas JSON bien formadas; falta manejo de respuestas malformadas en algunos casos (defensive parsing recomendable).

## Limitaciones UX y seguridad

- No existe protección CSRF ni autenticación basada en tokens; la sesión se maneja por comandos `login`/`logout` en el backend. Para uso real, considerar tokens y rutas seguras.
- Validación mínima de entrada en el frontend. El backend realiza validaciones adicionales, pero frontend debería validar formatos antes de enviar.

## Recomendaciones y mejoras

1. Configuración de BACKEND_URL por variable de entorno:
   - Añadir en `vite.config.ts` y usar `import.meta.env.VITE_BACKEND_URL` en componentes.

2. Centralizar tipos y servicios:
   - Crear `src/api.ts` con funciones `executeCommand`, `getDisks`, `getFiles`, `readFile`, `getJournaling`, `login`, `logout`.
   - Crear `src/types.ts` para `Disk`, `Partition`, `FileNode`, `Session`.

3. Manejo de sesión persistente:
   - Añadir endpoint `/session` en backend (si no existe) para consultar la sesión actual y sincronizar estado frontend al montar la app.
   - O guardar la sesión mínima (username, partitionId, isRoot) en `localStorage` y verificar con el backend al iniciar.

4. Tests y E2E:
   - Añadir tests unitarios para utilidades y componentes críticos.
   - Añadir E2E (Playwright / Cypress) para flujos: login → abrir visualizador → leer archivo → cerrar sesión.

5. Mejoras en UI:
   - Indicar claramente cuándo un comando requiere autenticación (inline en el editor) y qué comandos requieren `root`.
   - Mostrar help / cheatsheet con ejemplos de comandos comunes.

6. Robustez de peticiones:
   - Añadir timeouts y retries suaves para llamadas fetch.
   - Manejar estados de error más detallados y mostrar tooltips con posibles correcciones.

## Ejemplo de flujo end-to-end (pruebas manuales)

1. Levantar backend:

```bash
# desde backend/
go run main.go -server -port=8080
```

2. Levantar frontend:

```bash
cd front
npm install
npm run dev
```

3. Abrir navegador en `http://localhost:5173`.
4. En el editor colocar comandos:

```
# Crear disco y partición (si no existe aún)
mkdisk -size=10 -unit=M -path=/tmp/disco1.mia
fdisk -size=8 -unit=M -path=/tmp/disco1.mia -type=primaria -name=part1
mount -path=/tmp/disco1.mia -name=part1
```

5. Abrir "Iniciar Sesión", seleccionar la partición montada, usar usuario `root` con contraseña `123` (método por defecto tras mkfs).
6. Ejecutar `mkfile -path=/hola.txt -size=64` y luego ver archivo en `Visualizador` → explorar → abrir archivo.
7. Visualizar journaling con el botón `📜 Journaling`.

## Archivos relevantes (frontend)

- `front/package.json` — scripts y dependencias.
- `front/src/App.tsx` — App principal y lógica de ejecución de comandos.
- `front/src/components/*` — UI: `Login.tsx`, `FileSystemViewer.tsx`, `DiskSelector.tsx`, `PartitionViewer.tsx`, `FileExplorer.tsx`, `JournalingViewer.tsx`.
- `front/index.html`, `src/main.tsx` — punto de entrada.

## Próximos pasos sugeridos (priorizados)

1. Hacer `BACKEND_URL` configurable vía `VITE_BACKEND_URL` y reemplazar los hardcodes.
2. Extraer funciones de API a `src/api.ts` y centralizar manejo de errores.
3. Añadir `src/types.ts` y usar las mismas interfaces en todos los componentes.
4. Añadir endpoint `/session` (o `GET /health` mejorado) para recuperar estado de sesión y sincronizar con frontend.
5. Crear pruebas E2E básicas para flujo login → visualizar → leer archivo → logout.

---

Si quieres, puedo ahora:
- Generar automáticamente `src/api.ts` y `src/types.ts` con las funciones y tipos sugeridos y actualizar algunos componentes para consumirlos (pequeños cambios no invasivos).
- Añadir soporte para `VITE_BACKEND_URL` y cambiar todos los `BACKEND_URL` hardcodeados por la variable de entorno.
- Preparar una guía paso-a-paso con comandos exactos y salidas esperadas para usar en demo.

Dime cuál prefieres y lo hago en el siguiente paso.
