package commands

import (
	"backend/structs"
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// FileNode representa un archivo o carpeta para el frontend
type FileNode struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "file" o "folder"
	Size        int64  `json:"size"`
	Permissions string `json:"permissions"`
	Owner       string `json:"owner"`
	Group       string `json:"group"`
}

// GetFilesList obtiene la lista de archivos de un directorio
func GetFilesList(mounted *MountedPartition, dirPath string) ([]FileNode, error) {
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		return nil, err
	}

	// Normalizar la ruta
	dirPath = strings.TrimSpace(dirPath)
	if dirPath == "" {
		dirPath = "/"
	}

	var currentInodeNum int64 = 0 // Empezar desde la raíz

	// Si no es la raíz, navegar hasta el directorio
	if dirPath != "/" {
		parsedPath := parsePath(dirPath + "/dummy") // Agregar dummy para que parsee correctamente
		if parsedPath == nil {
			return nil, fmt.Errorf("ruta inválida: %s", dirPath)
		}

		fmt.Printf("   Navegando por directorios: %v\n", parsedPath.Directories)

		// Navegar por cada directorio
		for i, dirName := range parsedPath.Directories {
			fmt.Printf("   [%d] Buscando directorio: '%s' en inodo %d\n", i, dirName, currentInodeNum)

			nextInode, err := findInodeInDirectory(file, superblock, currentInodeNum, dirName)
			if err != nil {
				fmt.Printf("   ❌ ERROR: no se encontró '%s': %v\n", dirName, err)
				return nil, fmt.Errorf("no se encontró el directorio '%s': %v", dirName, err)
			}

			fmt.Printf("   ✅ Encontrado '%s' en inodo %d\n", dirName, nextInode)
			currentInodeNum = nextInode
		}
	}

	fmt.Printf("   📂 Leyendo contenido del inodo %d\n", currentInodeNum)

	// Leer el inodo del directorio
	var dirInode structs.Inodos
	inodePos := superblock.S_inode_start + (currentInodeNum * superblock.S_inode_s)
	file.Seek(inodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &dirInode); err != nil {
		return nil, fmt.Errorf("error al leer inodo del directorio: %v", err)
	}

	// Verificar que es un directorio
	if dirInode.I_type != '0' {
		return nil, fmt.Errorf("la ruta no es un directorio")
	}

	var files []FileNode

	// Leer todos los bloques del directorio
	for i := 0; i < 15 && dirInode.I_block[i] != -1; i++ {
		blockPos := superblock.S_block_start + (dirInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var dirBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &dirBlock); err != nil {
			continue
		}

		// Leer cada entrada del bloque
		for j := 0; j < 4; j++ {
			if dirBlock.BContent[j].BInodo == -1 {
				continue
			}

			// CORRECCIÓN: Extraer nombre correctamente
			entryName := strings.TrimRight(string(dirBlock.BContent[j].BName[:]), "\x00")
			entryName = strings.TrimSpace(entryName)

			fmt.Printf("   📄 Entrada encontrada: '%s' (inodo %d)\n", entryName, dirBlock.BContent[j].BInodo)

			// FILTRAR: Saltar entradas "." y ".."
			if entryName == "." || entryName == ".." {
				fmt.Printf("   ⏭️  Saltando entrada especial: '%s'\n", entryName)
				continue
			}

			// FILTRAR: Evitar duplicados
			isDuplicate := false
			for _, existingFile := range files {
				if existingFile.Name == entryName {
					isDuplicate = true
					fmt.Printf("   ⚠️  Entrada duplicada ignorada: '%s'\n", entryName)
					break
				}
			}
			if isDuplicate {
				continue
			}

			// Leer el inodo de la entrada
			entryInodePos := superblock.S_inode_start + (dirBlock.BContent[j].BInodo * superblock.S_inode_s)
			file.Seek(entryInodePos, 0)

			var entryInode structs.Inodos
			if err := binary.Read(file, binary.LittleEndian, &entryInode); err != nil {
				continue
			}

			// Crear FileNode
			fileNode := FileNode{
				Name:        entryName,
				Type:        getFileTypeFromInode(entryInode.I_type),
				Size:        entryInode.I_s,
				Permissions: getPermissionsStringFromBytes(entryInode.I_perm),
				Owner:       fmt.Sprintf("user%d", entryInode.I_uid),
				Group:       fmt.Sprintf("group%d", entryInode.I_gid),
			}

			files = append(files, fileNode)
			fmt.Printf("   ✅ Agregado: %s (tipo: %s)\n", fileNode.Name, fileNode.Type)
		}
	}

	fmt.Printf("   📊 Total de archivos válidos: %d\n", len(files))
	return files, nil
}

// getFileTypeFromInode convierte el tipo de inodo a string
func getFileTypeFromInode(itype byte) string {
	if itype == '0' {
		return "folder"
	}
	return "file"
}

// getPermissionsStringFromBytes convierte los permisos a formato rwxrwxrwx
func getPermissionsStringFromBytes(perms [3]byte) string {
	result := ""

	for _, perm := range perms {
		// Leer bits: 4=read, 2=write, 1=execute
		if perm&4 != 0 {
			result += "r"
		} else {
			result += "-"
		}
		if perm&2 != 0 {
			result += "w"
		} else {
			result += "-"
		}
		if perm&1 != 0 {
			result += "x"
		} else {
			result += "-"
		}
	}

	return result
}

// Leer el contenido completo de cualquier archivo (multi-bloque)
func ReadFileContent(mounted *MountedPartition, fileName string) (string, error) {
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return "", fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	// Obtener partición y superbloque
	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		return "", err
	}

	// Buscar el archivo en el directorio raíz
	inodeIndex, err := findFileInRootDirectory(file, superblock, fileName)
	if err != nil {
		return "", fmt.Errorf("archivo '%s' no encontrado: %v", fileName, err)
	}

	// Leer el inodo del archivo
	inodePosition := superblock.S_inode_start + (inodeIndex * superblock.S_inode_s)
	file.Seek(inodePosition, 0)
	var fileInode structs.Inodos
	if err := binary.Read(file, binary.LittleEndian, &fileInode); err != nil {
		return "", fmt.Errorf("error al leer el inodo de '%s': %v", fileName, err)
	}

	// Leer el contenido completo (multi-bloque)
	content, err := readFileContentMultiBlock(file, superblock, &fileInode)
	if err != nil {
		return "", fmt.Errorf("error al leer el contenido de '%s': %v", fileName, err)
	}

	return content, nil
}

// Escribir contenido completo a cualquier archivo (multi-bloque)
func WriteFileContent(mounted *MountedPartition, fileName, newContent string) error {
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	// Obtener partición y superbloque
	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		return err
	}

	// Buscar el archivo en el directorio raíz
	inodeIndex, err := findFileInRootDirectory(file, superblock, fileName)
	if err != nil {
		return fmt.Errorf("archivo '%s' no encontrado: %v", fileName, err)
	}

	// Leer el inodo del archivo
	inodePosition := superblock.S_inode_start + (inodeIndex * superblock.S_inode_s)
	file.Seek(inodePosition, 0)
	var fileInode structs.Inodos
	if err := binary.Read(file, binary.LittleEndian, &fileInode); err != nil {
		return fmt.Errorf("error al leer el inodo de '%s': %v", fileName, err)
	}

	// Escribir contenido multi-bloque
	err = writeFileContentMultiBlock(file, superblock, &fileInode, newContent, inodePosition)
	if err != nil {
		return fmt.Errorf("error al escribir '%s': %v", fileName, err)
	}

	return nil
}

func ReadUsersFileContent(mounted *MountedPartition) (string, error) {
	return ReadFileContent(mounted, "users.txt")
}

func WriteUsersFileContent(mounted *MountedPartition, newContent string) error {
	return WriteFileContent(mounted, "users.txt", newContent)
}

// Obtener partición y superbloque
func getPartitionAndSuperblock(file *os.File, mounted *MountedPartition) (*structs.Partition, *structs.SuperBloque, error) {
	// Primero: si el objeto mounted ya tiene el offset de inicio, intentar usarlo directamente.
	if mounted.Start > 0 {
		file.Seek(mounted.Start, 0)
		var sb structs.SuperBloque
		if err := binary.Read(file, binary.LittleEndian, &sb); err == nil {
			// Validar que parezca un superbloque válido
			if sb.S_inodes_count > 0 && sb.S_inode_s > 0 && sb.S_block_s > 0 {
				// Construir una partición basada en la información de mounted
				var p structs.Partition
				p.Part_start = mounted.Start
				p.Part_s = mounted.Size
				// Rellenar nombre (copiar hasta 16 bytes)
				var nameBytes [16]byte
				copy(nameBytes[:], []byte(mounted.Name))
				p.Part_name = nameBytes
				return &p, &sb, nil
			}
		}
		// Si falló, continuamos intentando buscar por nombre en el MBR
	}

	// Leer el MBR
	var mbr structs.MBR
	if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
		return nil, nil, fmt.Errorf("error al leer el MBR: %v", err)
	}

	// Encontrar la partición específica por nombre (por compatibilidad)
	var partition *structs.Partition
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
		return nil, nil, fmt.Errorf("no se pudo encontrar la partición '%s'", mounted.Name)
	}

	// Leer el superbloque
	file.Seek(partition.Part_start, 0)
	var superblock structs.SuperBloque
	if err := binary.Read(file, binary.LittleEndian, &superblock); err != nil {
		return nil, nil, fmt.Errorf("error al leer el superbloque: %v", err)
	}

	return partition, &superblock, nil
}

// Buscar archivo en el directorio raíz
func findFileInRootDirectory(file *os.File, superblock *structs.SuperBloque, fileName string) (int64, error) {
	file.Seek(superblock.S_block_start, 0)

	var rootBlock structs.BloqueCarpeta
	if err := binary.Read(file, binary.LittleEndian, &rootBlock); err != nil {
		return -1, fmt.Errorf("error al leer el directorio raíz: %v", err)
	}

	for i := 0; i < 4; i++ {
		if rootBlock.BContent[i].BInodo != -1 {
			entryName := string(rootBlock.BContent[i].BName[:])
			entryName = strings.Trim(entryName, "\x00")

			if entryName == fileName {
				return rootBlock.BContent[i].BInodo, nil
			}
		}
	}

	return -1, fmt.Errorf("archivo no encontrado")
}

// Leer contenido de archivo multi-bloque
func readFileContentMultiBlock(file *os.File, superblock *structs.SuperBloque, fileInode *structs.Inodos) (string, error) {
	var content strings.Builder

	for i := 0; i < 15; i++ {
		if fileInode.I_block[i] == -1 {
			break
		}

		blockPosition := superblock.S_block_start + (fileInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPosition, 0)

		var fileBlock structs.BloqueArchivo
		if err := binary.Read(file, binary.LittleEndian, &fileBlock); err != nil {
			return "", fmt.Errorf("error al leer el bloque %d: %v", i, err)
		}

		remainingBytes := fileInode.I_s - int64(content.Len())
		if remainingBytes <= 0 {
			break
		}

		var blockContent string
		if remainingBytes >= int64(len(fileBlock.BContent)) {
			blockContent = string(fileBlock.BContent[:])
		} else {
			blockContent = string(fileBlock.BContent[:remainingBytes])
		}

		blockContent = strings.TrimRight(blockContent, "\x00")
		content.WriteString(blockContent)

		if int64(content.Len()) >= fileInode.I_s {
			break
		}
	}

	result := content.String()
	if int64(len(result)) > fileInode.I_s {
		result = result[:fileInode.I_s]
	}

	return result, nil
}

// Escribir contenido de archivo multi-bloque
func writeFileContentMultiBlock(file *os.File, superblock *structs.SuperBloque, fileInode *structs.Inodos, newContent string, inodePosition int64) error {
	blockSize := len(structs.BloqueArchivo{}.BContent)
	contentBytes := []byte(newContent)
	blocksNeeded := (len(contentBytes) + blockSize - 1) / blockSize

	if blocksNeeded > 15 {
		return fmt.Errorf("el archivo es demasiado grande: necesita %d bloques, máximo 15", blocksNeeded)
	}

	// Actualizar el tamaño del archivo
	fileInode.I_s = int64(len(newContent))

	// Contar bloques actuales
	currentBlocks := 0
	for i := 0; i < 15; i++ {
		if fileInode.I_block[i] != -1 {
			currentBlocks++
		} else {
			break
		}
	}

	// Asignar bloques adicionales si es necesario
	if blocksNeeded > currentBlocks {
		for i := currentBlocks; i < blocksNeeded; i++ {
			newBlockIndex, err := findFreeBlock(file, superblock)
			if err != nil {
				return fmt.Errorf("no se pudo asignar bloque %d: %v", i, err)
			}
			fileInode.I_block[i] = newBlockIndex

			if err := markBlockAsUsed(file, superblock, newBlockIndex); err != nil {
				return fmt.Errorf("error al marcar bloque como usado: %v", err)
			}

		}
	}

	// Escribir el inodo actualizado
	file.Seek(inodePosition, 0)
	if err := binary.Write(file, binary.LittleEndian, fileInode); err != nil {
		return fmt.Errorf("error al escribir el inodo actualizado: %v", err)
	}

	// Escribir contenido en múltiples bloques
	for blockIndex := 0; blockIndex < blocksNeeded; blockIndex++ {
		if fileInode.I_block[blockIndex] == -1 {
			return fmt.Errorf("bloque %d no está asignado", blockIndex)
		}

		startByte := blockIndex * blockSize
		endByte := startByte + blockSize
		if endByte > len(contentBytes) {
			endByte = len(contentBytes)
		}

		blockContent := contentBytes[startByte:endByte]

		blockPosition := superblock.S_block_start + (fileInode.I_block[blockIndex] * superblock.S_block_s)
		file.Seek(blockPosition, 0)

		var fileBlock structs.BloqueArchivo
		for i := range fileBlock.BContent {
			fileBlock.BContent[i] = 0
		}

		copy(fileBlock.BContent[:], blockContent)

		if err := binary.Write(file, binary.LittleEndian, &fileBlock); err != nil {
			return fmt.Errorf("error al escribir el bloque %d: %v", blockIndex, err)
		}

	}

	return nil
}

// Buscar bloque libre
func findFreeBlock(file *os.File, superblock *structs.SuperBloque) (int64, error) {
	file.Seek(superblock.S_bm_block_start, 0)
	bitmap := make([]byte, superblock.S_blocks_count)
	if _, err := file.Read(bitmap); err != nil {
		return -1, fmt.Errorf("error al leer bitmap de bloques: %v", err)
	}

	for i := int64(0); i < superblock.S_blocks_count; i++ {
		if bitmap[i] == 0 {
			return i, nil
		}
	}

	return -1, fmt.Errorf("no hay bloques libres disponibles")
}

// Marcar bloque como usado
func markBlockAsUsed(file *os.File, superblock *structs.SuperBloque, blockIndex int64) error {
	bitmapPosition := superblock.S_bm_block_start + blockIndex
	file.Seek(bitmapPosition, 0)

	if _, err := file.Write([]byte{1}); err != nil {
		return fmt.Errorf("error al marcar bloque como usado: %v", err)
	}

	return nil
}

// Buscar inodo libre
func findFreeInode(file *os.File, superblock *structs.SuperBloque) (int64, error) {
	file.Seek(superblock.S_bm_inode_start, 0)
	bitmap := make([]byte, superblock.S_inodes_count)
	if _, err := file.Read(bitmap); err != nil {
		return -1, fmt.Errorf("error al leer bitmap de inodos: %v", err)
	}

	for i := int64(0); i < superblock.S_inodes_count; i++ {
		if bitmap[i] == 0 {
			return i, nil
		}
	}

	return -1, fmt.Errorf("no hay inodos libres disponibles")
}

// Marcar inodo como usado
func markInodeAsUsed(file *os.File, superblock *structs.SuperBloque, inodeIndex int64) error {
	bitmapPosition := superblock.S_bm_inode_start + inodeIndex
	file.Seek(bitmapPosition, 0)

	if _, err := file.Write([]byte{1}); err != nil {
		return fmt.Errorf("error al marcar inodo como usado: %v", err)
	}

	return nil
}

// Marcar inodo como libre
func markInodeAsFree(file *os.File, superblock *structs.SuperBloque, inodeIndex int64) error {
	bitmapPosition := superblock.S_bm_inode_start + inodeIndex
	file.Seek(bitmapPosition, 0)

	if _, err := file.Write([]byte{0}); err != nil {
		return fmt.Errorf("error al marcar inodo como libre: %v", err)
	}

	return nil
}

// Marcar bloque como libre
func markBlockAsFree(file *os.File, superblock *structs.SuperBloque, blockIndex int64) error {
	bitmapPosition := superblock.S_bm_block_start + blockIndex
	file.Seek(bitmapPosition, 0)

	if _, err := file.Write([]byte{0}); err != nil {
		return fmt.Errorf("error al marcar bloque como libre: %v", err)
	}

	return nil
}

// Estructura para parsear rutas
type ParsedPath struct {
	IsAbsolute  bool
	Directories []string
	FileName    string
	FullPath    string
}

// Busca y reemplaza la función parsePath:
func parsePath(path string) *ParsedPath {
	// Normalizar la ruta
	path = strings.TrimSpace(path)

	// Verificar que sea una ruta absoluta
	if !strings.HasPrefix(path, "/") {
		return nil
	}

	// Dividir la ruta en componentes
	parts := strings.Split(path, "/")

	// Filtrar elementos vacíos
	var cleanParts []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			cleanParts = append(cleanParts, part)
		}
	}

	if len(cleanParts) == 0 {
		// Es la raíz "/"
		return &ParsedPath{
			IsAbsolute:  true,
			Directories: []string{},
			FileName:    "",
			FullPath:    path,
		}
	}

	// El último elemento es el archivo
	fileName := cleanParts[len(cleanParts)-1]
	directories := cleanParts[:len(cleanParts)-1]

	return &ParsedPath{
		IsAbsolute:  true,
		Directories: directories,
		FileName:    fileName,
		FullPath:    path,
	}
}

// Buscar inodo en un directorio (función global)
func findInodeInDirectory(file *os.File, superblock *structs.SuperBloque, dirInodeIndex int64, itemName string) (int64, error) {
	// Leer el inodo del directorio
	inodePosition := superblock.S_inode_start + (dirInodeIndex * superblock.S_inode_s)
	file.Seek(inodePosition, 0)
	var dirInode structs.Inodos
	if err := binary.Read(file, binary.LittleEndian, &dirInode); err != nil {
		return -1, fmt.Errorf("error al leer inodo del directorio: %v", err)
	}

	// Verificar que es un directorio
	if dirInode.I_type != '0' {
		return -1, fmt.Errorf("no es un directorio")
	}

	// Buscar en todos los bloques del directorio
	for i := 0; i < 15; i++ {
		if dirInode.I_block[i] == -1 {
			break
		}

		blockPosition := superblock.S_block_start + (dirInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPosition, 0)

		var dirBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &dirBlock); err != nil {
			return -1, fmt.Errorf("error al leer bloque del directorio: %v", err)
		}

		// Buscar en las entradas del bloque
		for j := 0; j < 4; j++ {
			if dirBlock.BContent[j].BInodo != -1 {
				entryName := string(dirBlock.BContent[j].BName[:])
				entryName = strings.Trim(entryName, "\x00")

				// Comparación exacta incluyendo espacios
				if entryName == itemName {
					return dirBlock.BContent[j].BInodo, nil
				}

				// Comparación tolerante: comparar versiones truncadas a 12 caracteres
				e := entryName
				c := itemName
				if len(e) > 12 {
					e = e[:12]
				}
				if len(c) > 12 {
					c = c[:12]
				}
				if e == c {
					return dirBlock.BContent[j].BInodo, nil
				}
			}
		}
	}

	return -1, fmt.Errorf("elemento no encontrado")
}

// GetMountedPartitions devuelve las particiones montadas (función auxiliar)
func GetMountedPartitions() []MountedPartition {
	// Intentar cargar desde disco si está vacío
	if len(mountedPartitions) == 0 {
		// Llamar a la función que carga montajes (definida en mount.go)
		// la función loadMountsFromFile es package-local en mount.go
		// así que la llamamos indirectamente pidiendo que GetMountedPartition realice la carga
		// Intentamos un acceso no intrusivo
		_ = GetMountedPartition("")
	}
	return mountedPartitions
}

// ReadFileByPath lee el contenido de un archivo dado su path completo
func ReadFileByPath(mounted *MountedPartition, filePath string) (string, error) {
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return "", fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	// Obtener partición y superbloque
	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		return "", err
	}

	// Parsear el path
	parsed := parsePath(filePath)
	if parsed == nil {
		return "", fmt.Errorf("ruta inválida")
	}

	// Navegar por los directorios hasta llegar al archivo
	currentInodeIndex := int64(0) // Inodo raíz

	// Navegar por cada directorio en el path
	for _, dirName := range parsed.Directories {
		nextInode, err := findInodeInDirectory(file, superblock, currentInodeIndex, dirName)
		if err != nil {
			return "", fmt.Errorf("no se encontró el directorio '%s': %v", dirName, err)
		}
		currentInodeIndex = nextInode
	}

	// Ahora buscar el archivo en el directorio actual
	fileInodeIndex, err := findInodeInDirectory(file, superblock, currentInodeIndex, parsed.FileName)
	if err != nil {
		return "", fmt.Errorf("archivo '%s' no encontrado: %v", parsed.FileName, err)
	}

	// Leer el inodo del archivo
	inodePosition := superblock.S_inode_start + (fileInodeIndex * superblock.S_inode_s)
	file.Seek(inodePosition, 0)
	var fileInode structs.Inodos
	if err := binary.Read(file, binary.LittleEndian, &fileInode); err != nil {
		return "", fmt.Errorf("error al leer el inodo del archivo: %v", err)
	}

	// Verificar que sea un archivo y no una carpeta
	if fileInode.I_type == '0' {
		return "", fmt.Errorf("'%s' es una carpeta, no un archivo", parsed.FileName)
	}

	// Leer el contenido del archivo
	content, err := readFileContentMultiBlock(file, superblock, &fileInode)
	if err != nil {
		return "", fmt.Errorf("error al leer el contenido del archivo: %v", err)
	}

	return content, nil
}

// JournalEntry representa una entrada del journaling para el frontend
type JournalEntry struct {
	Operation   string `json:"operation"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	Timestamp   string `json:"timestamp"`
	User        string `json:"user"`
	Permissions string `json:"permissions"`
}

// GetJournaling obtiene todas las entradas del journaling basado en structs.Journal
func GetJournaling(mounted *MountedPartition) ([]JournalEntry, error) {
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	// Obtener partición y superbloque
	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		return nil, err
	}

	fmt.Println("========== DEBUG JOURNALING ==========")
	fmt.Printf("Partición: %s\n", mounted.Name)
	fmt.Printf("Tamaño de Journal struct: %d bytes\n", binary.Size(structs.Journal{}))
	fmt.Printf("S_inode_start: %d\n", superblock.S_inode_start)

	// Leer todas las entradas del journaling
	var entries []JournalEntry

	// Determinar layout según tipo de FS. Para EXT3 (S_file_system_type==3)
	// mkfs crea 50 entradas y las coloca inmediatamente después del superbloque.
	journalSize := int64(binary.Size(structs.Journal{}))
	journalingCount := 64
	if superblock.S_file_system_type == 3 {
		journalingCount = 50
	}

	// journalStart: according to mkfs/writeEXT3Structures and logToJournal,
	// the journaling region starts justo después del superbloque (partitionStart + superblockSize)
	// but we can compute it relative to bitmap positions stored in superblock.
	// Use S_bm_inode_start minus journalingCount*journalSize to get the start.
	journalStart := superblock.S_bm_inode_start - (int64(journalingCount) * journalSize)
	fmt.Printf("Journal start position: %d\n", journalStart)
	fmt.Printf("Tamaño calculado para %d journals: %d bytes\n", journalingCount, journalingCount*int(journalSize))
	fmt.Printf("Buscando %d entradas de journaling...\n\n", journalingCount)

	validEntries := 0
	// Helper: sanitize bytes -> printable string. Replace non-printable runes with '?'.
	sanitizeBytes := func(b []byte) string {
		// trim trailing zeros
		i := len(b)
		for i > 0 && b[i-1] == 0 {
			i--
		}
		b = b[:i]
		if len(b) == 0 {
			return ""
		}
		// decode runes safely, replace invalid/ non-printable with '?'
		var out []rune
		for len(b) > 0 {
			r, size := utf8.DecodeRune(b)
			if r == utf8.RuneError && size == 1 {
				out = append(out, '?')
				b = b[1:]
				continue
			}
			if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
				out = append(out, '?')
			} else {
				out = append(out, r)
			}
			b = b[size:]
		}
		return string(out)
	}

	for i := 0; i < journalingCount; i++ {
		journalPosition := journalStart + int64(i)*journalSize
		file.Seek(journalPosition, 0)

		// Leer raw block primero (nos permite buscar cadenas dentro del bloque si hay corrupción)
		raw := make([]byte, journalSize)
		if _, err := file.Read(raw); err != nil {
			fmt.Printf("  [%d] ❌ Error al leer bloque raw: %v\n", i, err)
			continue
		}
		var journal structs.Journal
		if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &journal); err != nil {
			fmt.Printf("  [%d] ❌ Error al decodificar struct: %v\n", i, err)
			// aún así podemos intentar extraer printable desde raw
		}

		fmt.Printf("  [%d] JCount=%d\n", i, journal.JCount)

		// Considerar entrada válida si JCount != -1 (sentinel de libre).
		if journal.JCount == -1 {
			if i < 5 {
				fmt.Printf("  [%d] ⏭️  JCount indica slot libre (-1), saltando...\n", i)
			}
			continue
		}

		// Extraer y sanear los campos (aceptar texto no-UTF8 y reemplazar problemas)
		operation := sanitizeBytes(journal.JContent.IOperation[:])
		path := sanitizeBytes(journal.JContent.IPath[:])
		content := sanitizeBytes(journal.JContent.IContent[:])

		// Helper: buscar la mayor secuencia ASCII imprimible dentro de bytes
		findBestPrintable := func(b []byte, minlen int) string {
			best := ""
			cur := make([]byte, 0, len(b))
			for _, ch := range b {
				if ch >= 32 && ch < 127 {
					cur = append(cur, ch)
				} else {
					if len(cur) >= minlen && len(cur) > len(best) {
						best = string(cur)
					}
					cur = cur[:0]
				}
			}
			if len(cur) >= minlen && len(cur) > len(best) {
				best = string(cur)
			}
			return best
		}

		// Si operation o path son cortos, buscar en el bloque raw
		if len(operation) < 3 {
			if bp := findBestPrintable(raw, 3); bp != "" {
				operation = bp
			}
		}
		if len(path) < 3 {
			if bp := findBestPrintable(raw, 3); bp != "" {
				path = bp
			}
		}

		// Tolerar timestamps inválidos: si <=0 o NaN, intentar extraer un uint32 little-endian
		// dentro del bloque que parezca un timestamp plausible.
		ts := journal.JContent.IDate
		if ts <= 0 || math.IsNaN(float64(ts)) {
			var foundTs uint32 = 0
			for k := 0; k+4 <= len(raw); k++ {
				v := binary.LittleEndian.Uint32(raw[k : k+4])
				if v >= 1000000000 && v <= 2000000000 {
					foundTs = v
					break
				}
			}
			if foundTs != 0 {
				ts = float32(foundTs)
			} else {
				ts = float32(time.Now().Unix())
			}
		}

		fmt.Printf("  [%d] ✅ ENTRADA (posible)\n", i)
		fmt.Printf("       Operation: '%s'\n", operation)
		fmt.Printf("       Path: '%s'\n", path)
		if len(content) > 30 {
			fmt.Printf("       Content: '%s...'\n", content[:30])
		} else {
			fmt.Printf("       Content: '%s'\n", content)
		}
		fmt.Printf("       IDate: %.0f\n", ts)
		fmt.Println()

		// Si ambos operation y path están vacíos, probablemente basura -> saltar
		if operation == "" && path == "" {
			fmt.Printf("  [%d] ⚠️  Operation y Path vacíos, saltando...\n", i)
			continue
		}

		validEntries++

		// Convertir timestamp float32 a string legible
		timestamp := fmt.Sprintf("%.0f", ts)

		entry := JournalEntry{
			Operation:   operation,
			Path:        path,
			Content:     content,
			Timestamp:   timestamp,
			User:        "N/A", // Tu struct Journal no tiene JOwner
			Permissions: "N/A", // Tu struct Journal no tiene permisos
		}

		entries = append(entries, entry)
	}

	fmt.Printf("\n========== RESUMEN ==========\n")
	fmt.Printf("📊 Total de entradas válidas encontradas: %d\n", validEntries)
	fmt.Printf("📦 Entradas retornadas al frontend: %d\n", len(entries))
	fmt.Println("==============================")

	// Si no hay entradas, devolver slice vacío
	if entries == nil {
		entries = []JournalEntry{}
	}

	return entries, nil
}
