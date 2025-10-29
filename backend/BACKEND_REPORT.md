# Reporte del backend — ExtreamFS (Simulación EXT2/EXT3)

> Documento técnico y guía de uso del backend del proyecto (simulación de sistemas de archivos EXT2/EXT3).

## Resumen

El backend está implementado en Go y provee dos modos de uso:
- CLI interactivo: ejecutable sin flags que abre un prompt donde se ingresan comandos.
- Servidor HTTP: ejecutable con `-server` que expone una API para el frontend.

El backend simula discos (archivos con extensión `.mia`), particiones (MBR/EBR), y sistemas de archivos estilo EXT2 y EXT3. Soporta creación de discos, particiones, formato (mkfs), montado, manejo de usuarios, creación/edición de archivos y reportes (Graphviz).

-----

## Requisitos y compilación

- Requiere Go (en `go.mod` se indica `go 1.24.5`).

Compilar:

```bash
# desde la carpeta backend
cd backend
go build -o extreamfs .
```

Ejecutar:

- Modo CLI (interactivo):

```bash
./extreamfs
# o
go run main.go
```

- Modo servidor HTTP:

```bash
./extreamfs -server -port=8080
# o
go run main.go -server -port=8080
```

-----

## API HTTP principal

El servidor HTTP expone handlers para que el frontend interactúe con el backend:

- POST /execute
  - Body: { "command": "mkdisk -size=10 -unit=M -path=/tmp/disk.mia" }
  - Ejecuta el comando como si viniera de la CLI. Retorna JSON con success/output/error.

- GET /health
  - Retorna estado del servicio.

- GET /disks
  - Retorna lista de discos registrados (lee un registro persistente en temp dir).

- GET /disks/mounted
  - Retorna solo particiones montadas (estructura ligera).

- POST /files
  - Body: { "partitionId": "50A", "path": "/" }
  - Lista archivos/entradas en la ruta solicitada (requiere sesión iniciada).

- POST /file/read
  - Body: { "partitionId": "50A", "path": "/foo.txt" }
  - Retorna contenido del archivo.

- POST /journaling
  - Body: { "partitionId": "50A" }
  - Retorna entradas de journaling para EXT3.

Las respuestas son JSON con campos `success`, `error`, `content` u otros según el handler.

-----

## Modo CLI (prompt)

El ejecutable sin `-server` abre un prompt interactivo con soporte para comentarios (líneas que empiezan con `#`) y eliminación de comentarios inline. El parsing de argumentos respeta comillas simples y dobles.

Uso básico: escribir el comando y sus flags. Ejemplo:

```
╰─➤ mkdisk -size=10 -unit=M -path=/home/user/disk.mia
╰─➤ mount -path=/home/user/disk.mia -name=part1
╰─➤ mkfs -id=50A -type=full -fs=3fs
╰─➤ login -user=root -pass=123 -id=50A
╰─➤ mkfile -path=/foo.txt -size=128
╰─➤ rep -name=sb -path=/tmp/sb.png -id=50A
```

-----

## Comandos disponibles (resumen)

Lista de comandos principales, flags obligatorios entre paréntesis:

- mkdisk -size, -unit (K|M), -fit (BF|FF|WF), -path (obligatorio)
  - Crea un archivo disco `.mia` y escribe un MBR.

- rmdisk -path (obligatorio)
  - Borra el archivo disco.

- fdisk -size, -unit, -path, -type (primaria|extendida), -fit, -name, -delete, -add
  - Crear/eliminar/ajustar particiones dentro del MBR/EBR.

- mount -path -name
  - Monta una partición (registro en memoria y actualización del MBR para particiones primarias).

- mounted
  - Lista particiones montadas (memoria).

- unmount -id
  - Desmonta por ID.

- mkfs -id -type (full) -fs (2fs|3fs)
  - Formatea la partición montada. `2fs` → EXT2, `3fs` → EXT3 (incluye journaling).

- login -user -pass -id
- logout
  - Gestión de sesión. `login` lee `users.txt` del FS para autenticar.

- mkgrp -name / rmgrp -name
- mkusr -user -pass -grp / rmusr -user
- chgrp -user -grp
  - Administración de grupos/usuarios (se almacenan en `users.txt`).

- mkdir -path [-p]
- mkfile -path [-r] -size -cont
- remove -path
- edit -path -contenido
- rename -path -name
- copy -path -destino
- move -path -destino
- cat -file1=... (hasta file10)
- find -path -name
- chown -path -usuario [-r]
- chmod -path -ugo [-r]

- rep -name -path -id [-path_file_ls]
  - Genera reportes (mbr, disk, inode, block, bm_inode, bm_block, tree, sb, file, ls). Requiere Graphviz para generar imágenes a partir de DOT.

- recovery -id
- loss -id
  - Operaciones relacionadas con recuperación y pérdida (herramientas incluidas en el backend).

-----

## Flujo típico (ejemplo corto)

1. Crear disco:
   mkdisk -size=20 -unit=M -path=/tmp/disk.mia

2. Crear partición (fdisk) y montar:
   fdisk -size=10 -unit=M -path=/tmp/disk.mia -type=primaria -name=part1
   mount -path=/tmp/disk.mia -name=part1

3. Formatear la partición montada (ID obtenido por `mounted`):
   mkfs -id=50A -type=full -fs=3fs

4. Iniciar sesión como root y crear archivos:
   login -user=root -pass=123 -id=50A
   mkfile -path=/notes.txt -size=128

5. Generar reporte:
   rep -name=sb -path=/tmp/superblock.png -id=50A

-----

## Estructuras internas importantes

Los tipos están en `backend/structs`.

- MBR
  - Estructura `structs.MBR` (contiene tamaño, fecha, signature y 4 particiones).
  - Las particiones en MBR usan `structs.Partition` (campos: status, type, start, size, name, etc.).

- EBR
  - `structs.EBR` para particiones lógicas encadenadas.

- SuperBloque (`structs.SuperBloque`)
  - Campos: tipo FS, contadores (inodos, bloques, libres), offsets a bitmaps e inodos, tamaños de inodo/bloque, magic (0xEF53), timestamps, etc.

- Inodos (`structs.Inodos`)
  - Campos: uid, gid, tamaño, tiempos (atime/ctime/mtime), 15 punteros (I_block), tipo (archivo/directorio) y permisos.

- Bloques
  - `BloqueCarpeta` — BContent[4] (4 entradas por bloque), cada entrada tiene `BName [12]byte` y `BInodo int64`.
  - `BloqueArchivo` — `BContent [64]byte` (64 bytes de datos por bloque).
  - `BloqueApuntador` — `BPointers [8]int64`.

- Journal (`structs.Journal` y `Information`)
  - Para EXT3, se reservan 50 entradas fijas. Cada entrada incluye operación, path, contenido (hasta 64 bytes) y fecha. Hay estructura `JournalRecovery` pensada para recuperar estado.

Tamaños y layouts:
- Los bloques de archivo son de 64 bytes según `BloqueArchivo`.
- `mkfs` calcula `n` (número de inodos) usando fórmulas en `commands/mkfs.go` y reserva: superbloque → journaling (si aplica) → bitmap inodos → bitmap bloques → inodos → bloques.

-----

## Formato del archivo disco (.mia)

- El archivo `.mia` contiene al inicio el MBR.
- Las particiones primarias y extendidas están en la tabla de 4 entradas del MBR; las lógicas se organizan con EBRs en su espacio.
- Cuando se formatea la partición (mkfs) se escribe el `SuperBloque` en el inicio de la partición (`partition.Part_start`), luego la estructura de journaling (EXT3), bitmaps e inodos y bloques.

-----

## Montado y registro de discos

- Las particiones montadas se mantienen en memoria en `commands.mountedPartitions`.
- El ID de partición se genera con `generatePartitionID` que usa un sufijo del carnet (`"50"`) + número de partición + letra (A,B,...). Ej: `505A` (formato: `50{n}{Letter}`).
- Para particiones primarias el MBR en disco se actualiza con `Part_id` y `Part_correlative`. Para particiones lógicas no se actualizan EBRs al montar (se busca en cadena de EBRs para platillos lógicos).
- Existe un registro persistente de discos en `os.TempDir()` con nombre `extreamfs_disk_registry.json` para recordar los discos creados (`commands/disk_registry.go`).

Nota: el estado de `mountedPartitions` se pierde al detener el backend (no es persistente). El registro de discos sí se mantiene en file temporal.

-----

## Users y sesiones

- `mkfs` crea un archivo `users.txt` en la raíz con líneas como:
  - `1,G,root`  → grupo root
  - `1,U,root,root,123` → usuario root (UID=1) en grupo root con contraseña `123`

- `login` lee `users.txt`, obtiene UID/GID y usa `StartSession` (almacena sesión en memoria en `commands.currentSession`).
- Muchas operaciones invocan `RequireActiveSession()` o `RequireRootPermission()` para validar permisos.

-----

## Journaling y recovery

- Para EXT3 se reservan 50 entradas de journal. La función `logToJournal` (y wrappers) escriben registros con la operación, path y contenido parcial.
- Existe la estructura `JournalRecovery` con campos pensados para snapshot del estado (bitmaps, inodos, bloques) para una posible recuperación, y comandos `recovery`/`loss` para operaciones relacionadas.

-----

## Reportes (rep)

El comando `rep` genera distintos reportes (MBR, DISK, INODE, BLOCK, BM_INODE, BM_BLOCK, TREE, SB, FILE, LS). Internamente:
- Lee MBR y Superbloeque mediante utilidades que intentan lectura en "mixed" endianness para robustez (`readMBRMixed`, `readSuperBlockMixed`).
- Produce HTML + DOT/Graphviz para imágenes.

Requisitos: tener `dot` (Graphviz) instalado si desea generar las imágenes.

-----

## Limitaciones conocidas y observaciones

- Montaje en memoria: `mountedPartitions` no persiste entre ejecuciones. Reiniciar el backend hará que pierda montajes.
- Registro de discos: guardado en archivo temporal; su ubicación puede variar entre máquinas.
- Endianness: el código incluye funciones que intentan read mixed-endian si la lectura directa falla; aún así podrían existir casos raros.
- Bloque de archivo pequeño (64 bytes) y bloque de carpeta con nombres limitados (12 bytes) — diseño simplificado para la simulación.
- El journal tiene tamaño fijo (50 entradas) — en un uso real habría políticas de rotación/overflow.

-----

## Ejemplos prácticos y sugerencias

1) Crear disco + partición primaria + montar + formatear + login + crear archivo + reporte:

```bash
cd backend
./extreamfs mkdisk -size=20 -unit=M -path=/tmp/disk.mia
# crear partición: (ejemplo)
./extreamfs fdisk -size=10 -unit=M -path=/tmp/disk.mia -type=primaria -name=part1
./extreamfs mount -path=/tmp/disk.mia -name=part1
# mirar montadas
./extreamfs mounted
# formatear (usar el ID mostrado por mounted, p.e. 50A)
./extreamfs mkfs -id=50A -type=full -fs=3fs
./extreamfs login -user=root -pass=123 -id=50A
./extreamfs mkfile -path=/readme.txt -size=128
./extreamfs rep -name=sb -path=/tmp/sb.png -id=50A
```

2) Ejecutar como servidor y usar frontend:

```bash
./extreamfs -server -port=8080
# luego el frontend hará POST a /execute y otras rutas descritas arriba
```

-----

## Archivos relevantes (ubicación)

- Código principal: `backend/main.go`
- Comandos: `backend/commands/*` (cada comando tiene su archivo: `mkdisk.go`, `mkfs.go`, `mkfile.go`, `mount.go`, `login.go`, `rep.go`, etc.)
- Structs/formatos: `backend/structs/*` (`super_bloque.go`, `inodos.go`, `bloques.go`, `journal.go`, `mbr.go`, `ebr.go`, etc.)
- Registro de discos persistente: archivo temporal `extreamfs_disk_registry.json` (ruta `os.TempDir()`)

-----

## Siguientes pasos recomendados

- Añadir persistencia de montajes (por ejemplo en el registro de discos o un archivo `mounted.json`) si se requiere continuidad entre reinicios.
- Agregar tests unitarios para las funciones críticas (`mkfs`, `mkfile`, `mount`, `rep`).
- Mejorar manejo de errores y logging estructurado.
- Documentar formato exacto del MBR/EBR/Partition con ejemplos hex (útil para depuración de archivos `.mia`).
- Añadir validaciones más estrictas sobre límites (tamaños, offsets) y manejo de concurrency si se expone la API HTTP en ambientes multi-usuario.

-----

## Conclusión

El backend proporciona una simulación completa para operaciones típicas de un sistema de archivos educativo (crear discos, particiones, formatear como EXT2/EXT3, journaling, manejo de usuarios/roles, reportes). La interacción puede hacerse por CLI o a través de la API HTTP (pensada para el frontend). El código está organizado por comandos y structs, con utilidades para lectura optimizada y generación de reportes.

Si quieres, hago a continuación cualquiera de estas acciones:
- Generar una guía de uso paso a paso con ejemplos concretos y salidas esperadas.
- Añadir tests (unitarios) que validen mkdisk → mount → mkfs → mkfile básicos.
- Documentar en detalle la estructura binaria del `.mia` (offsets y ejemplos hex) para debugging.

Indícame qué prefieres que haga después y sigo con ello.
