package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"
)

// ExecuteEdit - Editar el contenido de un archivo existente
func ExecuteEdit(path string, contenido string) {
	// Validar parámetros obligatorios
	if path == "" {
		fmt.Println("Error: el parámetro -path es obligatorio.")
		return
	}

	if contenido == "" {
		fmt.Println("Error: el parámetro -contenido es obligatorio.")
		return
	}

	// Validar que hay una sesión activa
	session := GetCurrentSession()
	if session == nil {
		fmt.Println("Error: no hay ninguna sesión activa. Use el comando 'login' primero.")
		return
	}

	// Normalizar la ruta del archivo destino
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Leer el contenido del archivo fuente del sistema operativo
	contentData, err := os.ReadFile(contenido)
	if err != nil {
		fmt.Printf("Error al leer el archivo de contenido '%s': %v\n", contenido, err)
		return
	}

	// Validar que el contenido no exceda el límite
	if len(contentData) > 64*1024 { // 64 KB máximo (considerando bloques)
		fmt.Printf("Error: el contenido es demasiado grande (máximo 64KB)\n")
		return
	}

	// Abrir el disco montado
	mounted := GetMountedPartition(session.PartitionID)
	if mounted == nil {
		fmt.Printf("Error: la partición '%s' no está montada.\n", session.PartitionID)
		return
	}

	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		fmt.Printf("Error al abrir el disco: %v\n", err)
		return
	}
	defer file.Close()

	// Obtener superbloque
	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		fmt.Printf("Error al obtener superbloque: %v\n", err)
		return
	}

	// Parsear la ruta
	parsedPath := parsePath(path)
	if parsedPath == nil {
		fmt.Printf("Error: ruta inválida '%s'.\n", path)
		return
	}

	// Buscar el archivo navegando por los directorios
	currentInodeNum := int64(0) // Inodo raíz

	// Navegar por los directorios
	for _, dirName := range parsedPath.Directories {
		nextInode, err := findInodeInDirectory(file, superblock, currentInodeNum, dirName)
		if err != nil {
			fmt.Printf("Error: no se encontró el directorio '%s': %v\n", dirName, err)
			return
		}
		currentInodeNum = nextInode
	}

	// Buscar el archivo en el directorio actual
	fileInodeNum, err := findInodeInDirectory(file, superblock, currentInodeNum, parsedPath.FileName)
	if err != nil {
		fmt.Printf("Error: no se encontró el archivo '%s': %v\n", parsedPath.FileName, err)
		return
	}

	// Leer el inodo del archivo
	var fileInode structs.Inodos
	fileInodePos := superblock.S_inode_start + (fileInodeNum * superblock.S_inode_s)
	file.Seek(fileInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &fileInode); err != nil {
		fmt.Printf("Error al leer el inodo del archivo: %v\n", err)
		return
	}

	// Verificar que es un archivo, no un directorio
	if fileInode.I_type == '0' {
		fmt.Printf("Error: '%s' es un directorio, no un archivo.\n", path)
		return
	}

	// Validar permisos de lectura y escritura
	if !checkReadWritePermissionOnInode(&fileInode, session.User, session.Group) {
		fmt.Printf("Error: no tiene permisos de lectura y escritura sobre '%s'.\n", path)
		return
	}

	// Liberar los bloques actuales del archivo
	if err := freeFileBlocks(file, superblock, &fileInode); err != nil {
		fmt.Printf("Error al liberar bloques del archivo: %v\n", err)
		return
	}

	// Escribir el nuevo contenido
	bytesWritten, err := writeFileContentEdit(file, superblock, &fileInode, contentData)
	if err != nil {
		fmt.Printf("Error al escribir el contenido: %v\n", err)
		return
	}

	// Actualizar el inodo del archivo
	fileInode.I_s = int64(bytesWritten)
	fileInode.I_mtime = time.Now().Unix()

	// Guardar el inodo actualizado
	file.Seek(fileInodePos, 0)
	if err := binary.Write(file, binary.LittleEndian, &fileInode); err != nil {
		fmt.Printf("Error al actualizar el inodo: %v\n", err)
		return
	}

	// Actualizar el superbloque
	file.Seek(superblock.S_inode_start-int64(binary.Size(structs.SuperBloque{})), 0)
	if err := binary.Write(file, binary.LittleEndian, superblock); err != nil {
		fmt.Printf("Error al actualizar el superbloque: %v\n", err)
		return
	}

	err = WriteJournal(
		mounted,
		"edit",
		path,
		string(contentData), // Convertir []byte a string
	)
	if err != nil {
		fmt.Printf("⚠️ No se pudo escribir al journal: %v\n", err)
	}

	fmt.Printf("✅ Archivo '%s' editado exitosamente.\n", path)
	fmt.Printf("   📝 %d bytes escritos.\n", bytesWritten)
}

// checkReadWritePermissionOnInode - Verificar permisos de lectura Y escritura
func checkReadWritePermissionOnInode(inode *structs.Inodos, username string, groupname string) bool {
	// Root siempre tiene permisos
	if username == "root" || username == "__auto__" {
		return true
	}

	// Extraer permisos desde el array de bytes
	permsStr := string(inode.I_perm[:])
	permsStr = strings.TrimRight(permsStr, "\x00")

	// Si no tiene permisos definidos, denegar acceso
	if len(permsStr) < 3 {
		return false
	}

	// Convertir caracteres ASCII a números
	ownerPerms := int(permsStr[0] - '0')  // Primer dígito: permisos del propietario
	groupPerms := int(permsStr[1] - '0')  // Segundo dígito: permisos del grupo
	othersPerms := int(permsStr[2] - '0') // Tercer dígito: permisos de otros

	// Convertir UID/GID del inodo a string para comparar
	inodeUID := fmt.Sprintf("%d", inode.I_uid)
	inodeGID := fmt.Sprintf("%d", inode.I_gid)

	// Usuario propietario - necesita permisos de lectura (4) Y escritura (2) = 6 o 7
	if username == inodeUID {
		return (ownerPerms&4) != 0 && (ownerPerms&2) != 0 // lectura Y escritura
	}

	// Grupo - necesita permisos de lectura Y escritura
	if groupname == inodeGID {
		return (groupPerms&4) != 0 && (groupPerms&2) != 0
	}

	// Otros - necesita permisos de lectura Y escritura
	return (othersPerms&4) != 0 && (othersPerms&2) != 0
}

// freeFileBlocks - Liberar todos los bloques de un archivo
func freeFileBlocks(file *os.File, superblock *structs.SuperBloque, inode *structs.Inodos) error {
	// Recorrer todos los bloques del archivo
	for i := 0; i < 15 && inode.I_block[i] != -1; i++ {
		// Marcar el bloque como libre
		if err := markBlockAsFree(file, superblock, inode.I_block[i]); err != nil {
			return err
		}
		superblock.S_free_blocks_count++

		// Limpiar la referencia del bloque
		inode.I_block[i] = -1
	}

	return nil
}

// writeFileContentEdit - Escribir contenido en un archivo (usa las funciones existentes)
func writeFileContentEdit(file *os.File, superblock *structs.SuperBloque, inode *structs.Inodos, content []byte) (int, error) {
	totalWritten := 0
	offset := 0
	blockIndex := 0

	for offset < len(content) && blockIndex < 15 {
		// Calcular cuánto escribir en este bloque
		remaining := len(content) - offset
		toWrite := remaining
		if toWrite > 64 {
			toWrite = 64 // Tamaño máximo de un bloque de archivo
		}

		// Buscar un bloque libre
		freeBlockNum, err := findFreeBlock(file, superblock)
		if err != nil {
			return totalWritten, fmt.Errorf("no hay bloques disponibles: %v", err)
		}

		// Marcar el bloque como usado
		if err := markBlockAsUsed(file, superblock, freeBlockNum); err != nil {
			return totalWritten, err
		}
		superblock.S_free_blocks_count--

		// Asignar el bloque al inodo
		inode.I_block[blockIndex] = freeBlockNum

		// Crear y escribir el bloque de archivo
		var fileBlock structs.BloqueArchivo
		copy(fileBlock.BContent[:], content[offset:offset+toWrite])

		// Escribir el bloque en el disco
		blockPos := superblock.S_block_start + (freeBlockNum * superblock.S_block_s)
		file.Seek(blockPos, 0)
		if err := binary.Write(file, binary.LittleEndian, &fileBlock); err != nil {
			return totalWritten, err
		}

		totalWritten += toWrite
		offset += toWrite
		blockIndex++
	}

	// Si hay más contenido que bloques disponibles
	if offset < len(content) {
		return totalWritten, fmt.Errorf("contenido demasiado grande, solo se escribieron %d bytes", totalWritten)
	}

	return totalWritten, nil
}
