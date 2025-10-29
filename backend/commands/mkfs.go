package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

func ExecuteMkfs(id string, formatType string, fs string) {
	// Normalizar parámetros
	formatType = strings.ToLower(formatType)
	fs = strings.ToLower(fs)

	if formatType == "" {
		formatType = "full" // Por defecto Full
	}

	if fs == "" {
		fs = "2fs" // Por defecto EXT2
	}

	if formatType != "full" {
		fmt.Printf("Error: Tipo de formateo '%s' no soportado. Use 'full'.\n", formatType)
		return
	}

	if fs != "2fs" && fs != "3fs" {
		fmt.Printf("Error: Sistema de archivos '%s' no soportado. Use '2fs' o '3fs'.\n", fs)
		return
	}

	fmt.Printf("Iniciando formateo %s con sistema %s de la partición...\n", strings.ToUpper(formatType), strings.ToUpper(fs))

	// Buscar la partición montada por ID
	mounted := GetMountedPartition(id)
	if mounted == nil {
		fmt.Printf("Error: No se encontró ninguna partición montada con ID '%s'.\n", id)
		return
	}

	// Debug información de la partición montada
	fmt.Printf("Partición montada encontrada:\n")
	fmt.Printf("   ID: %s\n", mounted.ID)
	fmt.Printf("   Nombre: '%s'\n", mounted.Name)
	fmt.Printf("   Ruta: %s\n", mounted.Path)
	fmt.Printf("   Tamaño: %d bytes\n", mounted.Size)

	// Abrir el archivo del disco
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		fmt.Printf("Error al abrir el disco: %v\n", err)
		return
	}
	defer file.Close()

	// Leer el MBR para obtener información de la partición
	// Preferir usar el offset de inicio almacenado en 'mounted' si está disponible
	var partition *structs.Partition
	if mounted.Start > 0 {
		// Construir una partición basada en mounted
		var p structs.Partition
		p.Part_start = mounted.Start
		p.Part_s = mounted.Size
		var nameBytes [16]byte
		copy(nameBytes[:], []byte(mounted.Name))
		p.Part_name = nameBytes
		partition = &p
	} else {
		// Leer el MBR para obtener información de la partición
		var mbr structs.MBR
		if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
			fmt.Printf("Error al leer el MBR: %v\n", err)
			return
		}

		// Encontrar la partición específica
		for _, p := range mbr.Mbr_partitions {
			if p.Part_status != '0' {
				partitionNameBytes := p.Part_name[:]
				nullIndex := len(partitionNameBytes)
				for j, b := range partitionNameBytes {
					if b == 0 {
						nullIndex = j
						break
					}
				}
				partitionName := strings.TrimSpace(string(partitionNameBytes[:nullIndex]))

				if strings.EqualFold(partitionName, mounted.Name) || partitionName == mounted.Name {
					partitionCopy := p
					partition = &partitionCopy
					break
				}
			}
		}

		if partition == nil {
			fmt.Printf("Error: No se pudo encontrar la partición '%s'.\n", mounted.Name)
			fmt.Printf("🔍 Particiones disponibles en el MBR:\n")
			for i, p := range mbr.Mbr_partitions {
				if p.Part_status != '0' {
					partitionNameBytes := p.Part_name[:]
					nullIndex := len(partitionNameBytes)
					for j, b := range partitionNameBytes {
						if b == 0 {
							nullIndex = j
							break
						}
					}
					partitionName := strings.TrimSpace(string(partitionNameBytes[:nullIndex]))
					fmt.Printf("   [%d] '%s' (status: %c, type: %c)\n", i, partitionName, p.Part_status, p.Part_type)
				}
			}
			return
		}
	}

	// Calcular el número de estructuras según el sistema de archivos
	var n int64
	var superblock structs.SuperBloque

	switch fs {
	case "2fs":
		// EXT2
		n = calculateEXT2Structures(partition.Part_s)
		fmt.Printf("Calculando estructuras EXT2 para partición de %d bytes...\n", partition.Part_s)
		fmt.Printf("   - Número de inodos: %d\n", n)
		fmt.Printf("   - Número de bloques: %d\n", n*3)

		superblock = createSuperblock(n, partition.Part_s, partition.Part_start)

		// Escribir las estructuras EXT2 en la partición
		if err := writeEXT2Structures(file, partition, superblock, n); err != nil {
			fmt.Printf("Error al escribir estructuras EXT2: %v\n", err)
			return
		}

	case "3fs":
		// EXT3
		n = calculateEXT3Structures(partition.Part_s)
		fmt.Printf("Calculando estructuras EXT3 para partición de %d bytes...\n", partition.Part_s)
		fmt.Printf("   - Número de inodos: %d\n", n)
		fmt.Printf("   - Número de bloques: %d\n", n*3)
		fmt.Printf("   - Entradas de Journaling: %d\n", 50)

		superblock = createSuperblockEXT3(n, partition.Part_s, partition.Part_start)

		// Escribir las estructuras EXT3 en la partición
		if err := writeEXT3Structures(file, partition, superblock, n); err != nil {
			fmt.Printf("Error al escribir estructuras EXT3: %v\n", err)
			return
		}
	}

	// Crear archivo users.txt en la raíz
	if err := createUsersFile(file, &superblock); err != nil {
		fmt.Printf("Error al crear archivo users.txt: %v\n", err)
		return
	}

	fmt.Printf("✅ Sistema de archivos %s creado exitosamente en partición '%s'.\n", strings.ToUpper(fs), mounted.Name)
	fmt.Printf("   ID: %s\n", id)
	fmt.Printf("   Tipo: %s\n", strings.ToUpper(formatType))
	fmt.Printf("   Sistema: %s\n", strings.ToUpper(fs))
	fmt.Printf("   Inodos: %d\n", n)
	fmt.Printf("   Bloques: %d\n", n*3)
	if fs == "3fs" {
		fmt.Printf("   Journaling: 50 entradas\n")
	}
	fmt.Printf("   Archivo users.txt creado en la raíz\n")
}

// Calcular número de estructuras según la fórmula EXT3
func calculateEXT3Structures(partitionSize int64) int64 {
	const journalingCount = 50 // Constante fija

	superblockSize := int64(binary.Size(structs.SuperBloque{}))
	journalSize := int64(binary.Size(structs.Journal{}))
	inodeSize := int64(binary.Size(structs.Inodos{}))

	carpetaSize := int64(binary.Size(structs.BloqueCarpeta{}))
	archivoSize := int64(binary.Size(structs.BloqueArchivo{}))

	var blockSize int64
	if carpetaSize > archivoSize {
		blockSize = carpetaSize
	} else {
		blockSize = archivoSize
	}

	// Fórmula EXT3: tamaño_particion = superblock + 50*journal + n + 3n + n*inode + 3n*block
	// n = (tamaño_particion - superblock - 50*journal) / (1 + 3 + inode + 3*block)

	numerator := float64(partitionSize - superblockSize - (journalingCount * journalSize))
	denominator := float64(1 + 3 + inodeSize + 3*blockSize)

	n := math.Floor(numerator / denominator)

	if n <= 0 {
		n = 1
	}

	return int64(n)
}

// Crear superbloque para EXT3
func createSuperblockEXT3(inodeCount int64, _ int64, partitionStart int64) structs.SuperBloque {
	const journalingCount = 50
	blockCount := inodeCount * 3
	now := time.Now().Unix()

	superblockSize := int64(binary.Size(structs.SuperBloque{}))
	journalSize := int64(binary.Size(structs.Journal{}))

	// Estructura EXT3: Superbloque → Journaling → Bitmap Inodos → Bitmap Bloques → Inodos → Bloques
	journalingStart := partitionStart + superblockSize
	bitmapInodesStart := journalingStart + (journalingCount * journalSize)
	bitmapBlocksStart := bitmapInodesStart + inodeCount
	inodesStart := bitmapBlocksStart + blockCount

	carpetaSize := int64(binary.Size(structs.BloqueCarpeta{}))
	archivoSize := int64(binary.Size(structs.BloqueArchivo{}))

	var blockRealSize int64
	if carpetaSize > archivoSize {
		blockRealSize = carpetaSize
	} else {
		blockRealSize = archivoSize
	}

	blocksStart := inodesStart + inodeCount*int64(binary.Size(structs.Inodos{}))

	return structs.SuperBloque{
		S_file_system_type:  3, // EXT3
		S_inodes_count:      inodeCount,
		S_blocks_count:      blockCount,
		S_free_blocks_count: blockCount - 2,
		S_free_inodes_count: inodeCount - 2,
		S_mtime:             now,
		S_umtime:            now,
		S_mnt_count:         1,
		S_magic:             0xEF53,
		S_inode_s:           int64(binary.Size(structs.Inodos{})),
		S_block_s:           blockRealSize,
		S_first_ino:         2,
		S_first_blo:         2,
		S_bm_inode_start:    bitmapInodesStart,
		S_bm_block_start:    bitmapBlocksStart,
		S_inode_start:       inodesStart,
		S_block_start:       blocksStart,
	}
}

// Escribir todas las estructuras EXT3 en la partición
func writeEXT3Structures(file *os.File, partition *structs.Partition, superblock structs.SuperBloque, n int64) error {
	const journalingCount = 50

	// Posicionarse al inicio de la partición
	file.Seek(partition.Part_start, 0)

	// 1. Escribir Superbloque
	if err := binary.Write(file, binary.LittleEndian, &superblock); err != nil {
		return fmt.Errorf("error escribiendo superbloque: %v", err)
	}

	// 2. Escribir Journaling (50 entradas vacías)
	emptyJournal := structs.Journal{
		JCount:   -1,
		JContent: structs.Information{},
	}

	for i := 0; i < journalingCount; i++ {
		if err := binary.Write(file, binary.LittleEndian, &emptyJournal); err != nil {
			return fmt.Errorf("error escribiendo journaling: %v", err)
		}
	}

	// 3. Escribir Bitmap de Inodos (inicializado en 0, excepto el primer inodo)
	bitmapInodes := make([]byte, n)
	bitmapInodes[0] = 1
	if _, err := file.Write(bitmapInodes); err != nil {
		return fmt.Errorf("error escribiendo bitmap de inodos: %v", err)
	}

	// 4. Escribir Bitmap de Bloques (inicializado en 0, excepto el primer bloque)
	bitmapBlocks := make([]byte, n*3)
	bitmapBlocks[0] = 1
	if _, err := file.Write(bitmapBlocks); err != nil {
		return fmt.Errorf("error escribiendo bitmap de bloques: %v", err)
	}

	// 5. Escribir Inodos (inicializar el inodo raíz)
	rootInode := createRootInode()
	if err := binary.Write(file, binary.LittleEndian, &rootInode); err != nil {
		return fmt.Errorf("error escribiendo inodo raíz: %v", err)
	}

	// Inicializar todos los inodos restantes
	emptyInode := structs.Inodos{}
	for i := 0; i < 15; i++ {
		emptyInode.I_block[i] = -1
	}

	for i := int64(1); i < n; i++ {
		if err := binary.Write(file, binary.LittleEndian, &emptyInode); err != nil {
			return fmt.Errorf("error escribiendo inodos vacíos: %v", err)
		}
	}

	// 6. Escribir Bloques (inicializar el bloque raíz)
	rootBlock := createRootBlock()
	if err := binary.Write(file, binary.LittleEndian, &rootBlock); err != nil {
		return fmt.Errorf("error escribiendo bloque raíz: %v", err)
	}

	// Inicializar todos los bloques restantes
	emptyBlock := structs.BloqueArchivo{}
	for i := range emptyBlock.BContent {
		emptyBlock.BContent[i] = 0
	}

	for i := int64(1); i < n*3; i++ {
		if err := binary.Write(file, binary.LittleEndian, &emptyBlock); err != nil {
			return fmt.Errorf("error escribiendo bloques vacíos: %v", err)
		}
	}

	return nil
}

// Registrar operación en el journal (solo para EXT3)
func logToJournal(file *os.File, superblock *structs.SuperBloque, operation string, path string, content string) error {
	// Solo registrar si es EXT3
	if superblock.S_file_system_type != 3 {
		return nil
	}

	// Calcular posición del journaling (después del superbloque)
	journalSize := int64(binary.Size(structs.Journal{}))

	// El journaling empieza después del superbloque
	journalStart := superblock.S_bm_inode_start - (50 * journalSize)

	// Buscar primera entrada libre
	var journal structs.Journal
	for i := int64(0); i < 50; i++ {
		file.Seek(journalStart+(i*journalSize), 0)
		binary.Read(file, binary.LittleEndian, &journal)

		if journal.JCount == -1 {
			// Entrada libre encontrada
			// Normalizar JCount: usar index+1 para indicar ocupada (consistente con WriteJournal)
			journal.JCount = int32(i + 1)

			// Llenar información
			copy(journal.JContent.IOperation[:], []byte(operation))
			copy(journal.JContent.IPath[:], []byte(path))

			// Solo copiar hasta el límite del array
			contentBytes := []byte(content)
			if len(contentBytes) > 64 {
				contentBytes = contentBytes[:64]
			}
			copy(journal.JContent.IContent[:], contentBytes)
			journal.JContent.IDate = float32(time.Now().Unix())

			// Escribir entrada
			file.Seek(journalStart+(i*journalSize), 0)
			return binary.Write(file, binary.LittleEndian, &journal)
		}
	}

	return fmt.Errorf("journal lleno")
}

// Crear archivo users.txt en la raíz
func createUsersFile(file *os.File, superblock *structs.SuperBloque) error {
	// Contenido inicial del archivo users.txt
	usersContent := "1,G,root\n1,U,root,root,123\n"

	// Buscar inodo libre (índice 1, ya que 0 es el directorio raíz)
	inodeIndex := int64(1)

	// Crear inodo para el archivo users.txt
	now := time.Now().Unix()
	fileInode := structs.Inodos{
		I_uid:   0,                        // Usuario root
		I_gid:   0,                        // Grupo root
		I_s:     int64(len(usersContent)), // Tamaño del contenido
		I_atime: now,
		I_ctime: now,
		I_mtime: now,
		I_type:  '1',                    // Archivo (no directorio)
		I_perm:  [3]byte{'6', '4', '4'}, // Permisos 644
	}

	// El primer bloque del archivo apunta al bloque 1 (bloque 0 es directorio raíz)
	fileInode.I_block[0] = 1

	// Inicializar el resto de bloques en -1
	for i := 1; i < 15; i++ {
		fileInode.I_block[i] = -1
	}

	// Debug posiciones
	inodePosition := superblock.S_inode_start + (inodeIndex * superblock.S_inode_s)
	blockPosition := superblock.S_block_start + (1 * superblock.S_block_s)

	// Escribir el inodo en la posición correcta
	file.Seek(inodePosition, 0)
	if err := binary.Write(file, binary.LittleEndian, &fileInode); err != nil {
		return fmt.Errorf("error escribiendo inodo users.txt: %v", err)
	}

	// Crear bloque de archivo con el contenido
	fileBlock := structs.BloqueArchivo{}

	// Limpiar explícitamente todo el bloque primero
	for i := range fileBlock.BContent {
		fileBlock.BContent[i] = 0
	}

	// Ahora copiar el contenido
	copy(fileBlock.BContent[:], []byte(usersContent))

	// Escribir el bloque en la posición correcta (bloque 1)
	file.Seek(blockPosition, 0)
	if err := binary.Write(file, binary.LittleEndian, &fileBlock); err != nil {
		return fmt.Errorf("error escribiendo bloque users.txt: %v", err)
	}

	// Actualizar el directorio raíz para incluir la entrada de users.txt
	if err := addFileToRootDirectory(file, superblock, "users.txt", inodeIndex); err != nil {
		return fmt.Errorf("error agregando users.txt al directorio raíz: %v", err)
	}

	// Actualizar bitmaps
	if err := updateBitmaps(file, superblock, inodeIndex, 1); err != nil {
		return fmt.Errorf("error actualizando bitmaps: %v", err)
	}

	// Registrar en el journal si es EXT3
	if superblock.S_file_system_type == 3 {
		if err := logToJournal(file, superblock, "mkfile", "/users.txt", usersContent); err != nil {
			fmt.Printf("⚠️  Advertencia: error registrando en journal: %v\n", err)
		}
	}

	// Actualizar contadores en superbloque
	superblock.S_free_inodes_count--
	superblock.S_free_blocks_count--
	superblock.S_first_ino = 2 // Siguiente inodo libre
	superblock.S_first_blo = 2 // Siguiente bloque libre

	return nil
}

// Agregar archivo al directorio raíz
func addFileToRootDirectory(file *os.File, superblock *structs.SuperBloque, fileName string, inodeIndex int64) error {
	// Leer el bloque del directorio raíz (bloque 0)
	file.Seek(superblock.S_block_start, 0)

	var rootBlock structs.BloqueCarpeta
	if err := binary.Read(file, binary.LittleEndian, &rootBlock); err != nil {
		return err
	}

	// Validar longitud del nombre
	if len(fileName) > 12 {
		return fmt.Errorf("nombre de archivo demasiado largo: máximo 12 caracteres")
	}

	// Limpiar la entrada en posición 2
	for j := range rootBlock.BContent[2].BName {
		rootBlock.BContent[2].BName[j] = 0
	}

	// Copiar el nombre del archivo
	copy(rootBlock.BContent[2].BName[:], []byte(fileName))
	rootBlock.BContent[2].BInodo = inodeIndex

	fmt.Printf("✅ Archivo '%s' agregado en posición 2\n", fileName)

	// Escribir el bloque actualizado
	file.Seek(superblock.S_block_start, 0)
	return binary.Write(file, binary.LittleEndian, &rootBlock)
}

// Actualizar bitmaps
func updateBitmaps(file *os.File, superblock *structs.SuperBloque, inodeIndex int64, blockIndex int64) error {
	// Actualizar bitmap de inodos
	file.Seek(superblock.S_bm_inode_start+inodeIndex, 0)
	if _, err := file.Write([]byte{1}); err != nil {
		return err
	}

	// Actualizar bitmap de bloques
	file.Seek(superblock.S_bm_block_start+blockIndex, 0)
	if _, err := file.Write([]byte{1}); err != nil {
		return err
	}

	return nil
}

// Calcular número de estructuras según la fórmula EXT2
func calculateEXT2Structures(partitionSize int64) int64 {
	// Tamaños de las estructuras
	superblockSize := int64(binary.Size(structs.SuperBloque{}))
	inodeSize := int64(binary.Size(structs.Inodos{}))

	// Usar el tamaño del bloque más grande
	carpetaSize := int64(binary.Size(structs.BloqueCarpeta{}))
	archivoSize := int64(binary.Size(structs.BloqueArchivo{}))

	var blockSize int64
	if carpetaSize > archivoSize {
		blockSize = carpetaSize
	} else {
		blockSize = archivoSize
	}

	// Fórmula: tamaño_particion = sizeOf(superblock) + n + 3*n + n*sizeOf(inodos) + 3*n*sizeOf(block)
	// Despejando n: n = (tamaño_particion - sizeOf(superblock)) / (1 + 3 + sizeOf(inodos) + 3*sizeOf(block))

	numerator := float64(partitionSize - superblockSize)
	denominator := float64(1 + 3 + inodeSize + 3*blockSize)

	n := math.Floor(numerator / denominator)

	// Asegurar que n sea positivo y razonable
	if n <= 0 {
		n = 1
	}

	return int64(n)
}

// Crear superbloque con valores iniciales
func createSuperblock(inodeCount int64, _ int64, partitionStart int64) structs.SuperBloque {
	blockCount := inodeCount * 3
	now := time.Now().Unix()

	// Calcular posiciones de las estructuras
	superblockSize := int64(binary.Size(structs.SuperBloque{}))
	bitmapInodesStart := partitionStart + superblockSize
	bitmapBlocksStart := bitmapInodesStart + inodeCount
	inodesStart := bitmapBlocksStart + blockCount

	// Usar el tamaño real del bloque más grande
	carpetaSize := int64(binary.Size(structs.BloqueCarpeta{}))
	archivoSize := int64(binary.Size(structs.BloqueArchivo{}))

	var blockRealSize int64
	if carpetaSize > archivoSize {
		blockRealSize = carpetaSize
	} else {
		blockRealSize = archivoSize
	}

	blocksStart := inodesStart + inodeCount*int64(binary.Size(structs.Inodos{}))

	return structs.SuperBloque{
		S_file_system_type:  2, // EXT2
		S_inodes_count:      inodeCount,
		S_blocks_count:      blockCount,
		S_free_blocks_count: blockCount - 2,
		S_free_inodes_count: inodeCount - 2,
		S_mtime:             now,
		S_umtime:            now,
		S_mnt_count:         1,
		S_magic:             0xEF53,
		S_inode_s:           int64(binary.Size(structs.Inodos{})),
		S_block_s:           blockRealSize,
		S_first_ino:         2,
		S_first_blo:         2,
		S_bm_inode_start:    bitmapInodesStart,
		S_bm_block_start:    bitmapBlocksStart,
		S_inode_start:       inodesStart,
		S_block_start:       blocksStart,
	}
}

// Escribir todas las estructuras EXT2 en la partición
func writeEXT2Structures(file *os.File, partition *structs.Partition, superblock structs.SuperBloque, n int64) error {
	// Posicionarse al inicio de la partición
	file.Seek(partition.Part_start, 0)

	// 1. Escribir Superbloque
	if err := binary.Write(file, binary.LittleEndian, &superblock); err != nil {
		return fmt.Errorf("error escribiendo superbloque: %v", err)
	}

	// 2. Escribir Bitmap de Inodos (inicializado en 0, excepto el primer inodo)
	bitmapInodes := make([]byte, n)
	bitmapInodes[0] = 1
	if _, err := file.Write(bitmapInodes); err != nil {
		return fmt.Errorf("error escribiendo bitmap de inodos: %v", err)
	}

	// 3. Escribir Bitmap de Bloques (inicializado en 0, excepto el primer bloque)
	bitmapBlocks := make([]byte, n*3)
	bitmapBlocks[0] = 1
	if _, err := file.Write(bitmapBlocks); err != nil {
		return fmt.Errorf("error escribiendo bitmap de bloques: %v", err)
	}

	// 4. Escribir Inodos (inicializar el inodo raíz)
	rootInode := createRootInode()
	if err := binary.Write(file, binary.LittleEndian, &rootInode); err != nil {
		return fmt.Errorf("error escribiendo inodo raíz: %v", err)
	}

	// Inicializar todos los inodos restantes (incluyendo posición 1)
	emptyInode := structs.Inodos{}
	for i := 0; i < 15; i++ {
		emptyInode.I_block[i] = -1
	}

	for i := int64(1); i < n; i++ {
		if err := binary.Write(file, binary.LittleEndian, &emptyInode); err != nil {
			return fmt.Errorf("error escribiendo inodos vacíos: %v", err)
		}
	}

	// 5. Escribir Bloques (inicializar el bloque raíz)
	rootBlock := createRootBlock()
	if err := binary.Write(file, binary.LittleEndian, &rootBlock); err != nil {
		return fmt.Errorf("error escribiendo bloque raíz: %v", err)
	}

	// Inicializar todos los bloques restantes (incluyendo posición 1)
	emptyBlock := structs.BloqueArchivo{}
	for i := range emptyBlock.BContent {
		emptyBlock.BContent[i] = 0
	}

	for i := int64(1); i < n*3; i++ {
		if err := binary.Write(file, binary.LittleEndian, &emptyBlock); err != nil {
			return fmt.Errorf("error escribiendo bloques vacíos: %v", err)
		}
	}

	return nil
}

// Crear el inodo raíz (directorio raíz)
func createRootInode() structs.Inodos {
	now := time.Now().Unix() // Unix timestamp

	inode := structs.Inodos{
		I_uid:   0,                      // Usuario root
		I_gid:   0,                      // Grupo root
		I_s:     0,                      // Tamaño inicial
		I_atime: now,                    // Unix timestamp
		I_ctime: now,                    // Unix timestamp
		I_mtime: now,                    // Unix timestamp
		I_type:  '0',                    // Directorio
		I_perm:  [3]byte{'7', '5', '5'}, // Permisos 755
	}

	// El primer bloque apunta al bloque de directorio raíz
	inode.I_block[0] = 0

	// Inicializar el resto de bloques en -1
	for i := 1; i < 15; i++ {
		inode.I_block[i] = -1
	}

	return inode
}

// Crear el bloque raíz (directorio raíz con . y ..)
func createRootBlock() structs.BloqueCarpeta {
	var block structs.BloqueCarpeta

	// Inicializar todas las entradas explícitamente
	for i := 0; i < 4; i++ {
		// Limpiar completamente el nombre
		for j := range block.BContent[i].BName {
			block.BContent[i].BName[j] = 0
		}
		// Marcar como libre
		block.BContent[i].BInodo = -1
	}

	// Entrada para "." (directorio actual)
	copy(block.BContent[0].BName[:], []byte("."))
	block.BContent[0].BInodo = 0

	// Entrada para ".." (directorio padre)
	copy(block.BContent[1].BName[:], []byte(".."))
	block.BContent[1].BInodo = 0

	return block
}
