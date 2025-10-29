package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"
)

// ExecuteCopy - Copiar archivo o carpeta con su contenido a otro destino
func ExecuteCopy(path string, destino string) {
	// Validar parámetros obligatorios
	if path == "" {
		fmt.Println("Error: el parámetro -path es obligatorio.")
		return
	}

	if destino == "" {
		fmt.Println("Error: el parámetro -destino es obligatorio.")
		return
	}

	// Validar que hay una sesión activa
	session := GetCurrentSession()
	if session == nil {
		fmt.Println("Error: no hay ninguna sesión activa. Use el comando 'login' primero.")
		return
	}

	// Normalizar rutas
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	destino = strings.TrimSpace(destino)
	if !strings.HasPrefix(destino, "/") {
		destino = "/" + destino
	}

	// Validar que destino no sea igual a origen
	if path == destino {
		fmt.Println("Error: el origen y el destino no pueden ser iguales.")
		return
	}

	// Validar que no se intente copiar una ruta dentro de sí misma
	if strings.HasPrefix(destino, path+"/") {
		fmt.Println("Error: no se puede copiar una carpeta dentro de sí misma.")
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

	// Parsear la ruta de origen
	parsedPath := parsePath(path)
	if parsedPath == nil {
		fmt.Printf("Error: ruta de origen inválida '%s'.\n", path)
		return
	}

	// Buscar el archivo/carpeta de origen
	sourceInodeNum, err := findItemByPath(file, superblock, parsedPath)
	if err != nil {
		fmt.Printf("Error: no se encontró '%s': %v\n", path, err)
		return
	}

	// Leer el inodo de origen
	var sourceInode structs.Inodos
	sourceInodePos := superblock.S_inode_start + (sourceInodeNum * superblock.S_inode_s)
	file.Seek(sourceInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &sourceInode); err != nil {
		fmt.Printf("Error al leer el inodo de origen: %v\n", err)
		return
	}

	// Validar permisos de lectura sobre el origen
	if !checkReadPermissionOnInode(&sourceInode, session.User, session.Group) {
		fmt.Printf("Error: no tiene permisos de lectura sobre '%s'.\n", path)
		return
	}

	// Parsear la ruta de destino
	parsedDestino := parsePath(destino)
	if parsedDestino == nil {
		fmt.Printf("Error: ruta de destino inválida '%s'.\n", destino)
		return
	}

	// Buscar el directorio de destino
	destInodeNum, err := findItemByPath(file, superblock, parsedDestino)
	if err != nil {
		fmt.Printf("Error: no se encontró el directorio de destino '%s': %v\n", destino, err)
		return
	}

	// Leer el inodo de destino
	var destInode structs.Inodos
	destInodePos := superblock.S_inode_start + (destInodeNum * superblock.S_inode_s)
	file.Seek(destInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &destInode); err != nil {
		fmt.Printf("Error al leer el inodo de destino: %v\n", err)
		return
	}

	// Validar que el destino sea un directorio
	if destInode.I_type != '0' {
		fmt.Printf("Error: el destino '%s' no es un directorio.\n", destino)
		return
	}

	// Validar permisos de escritura sobre el destino
	if !checkWritePermissionOnInode(&destInode, session.User, session.Group) {
		fmt.Printf("Error: no tiene permisos de escritura sobre el directorio de destino '%s'.\n", destino)
		return
	}

	// Verificar que no exista ya un archivo/carpeta con el mismo nombre en el destino
	existingInode, _ := findInodeInDirectory(file, superblock, destInodeNum, parsedPath.FileName)
	if existingInode != -1 {
		fmt.Printf("Error: ya existe '%s' en el directorio de destino.\n", parsedPath.FileName)
		return
	}

	// Realizar la copia
	fmt.Printf("📋 Copiando '%s' a '%s'...\n", path, destino)

	copiedCount := 0
	skippedCount := 0

	if sourceInode.I_type == '0' { // Es un directorio
		newDirInodeNum, err := copyDirectoryRecursive(file, superblock, sourceInodeNum, destInodeNum, parsedPath.FileName, session.User, session.Group, &copiedCount, &skippedCount)
		if err != nil {
			fmt.Printf("Error al copiar el directorio: %v\n", err)
			return
		}

		// Agregar la entrada del nuevo directorio al directorio de destino
		if err := addEntryToDirectory(file, superblock, destInodeNum, parsedPath.FileName, newDirInodeNum); err != nil {
			fmt.Printf("Error al agregar la entrada al directorio de destino: %v\n", err)
			return
		}

	} else { // Es un archivo
		newFileInodeNum, err := copyFile(file, superblock, &sourceInode)
		if err != nil {
			fmt.Printf("Error al copiar el archivo: %v\n", err)
			return
		}
		copiedCount++

		// Agregar la entrada del archivo al directorio de destino
		if err := addEntryToDirectory(file, superblock, destInodeNum, parsedPath.FileName, newFileInodeNum); err != nil {
			fmt.Printf("Error al agregar la entrada al directorio de destino: %v\n", err)
			return
		}
	}

	// Actualizar el superbloque
	file.Seek(superblock.S_inode_start-int64(binary.Size(structs.SuperBloque{})), 0)
	if err := binary.Write(file, binary.LittleEndian, superblock); err != nil {
		fmt.Printf("Error al actualizar el superbloque: %v\n", err)
		return
	}

	fmt.Printf("✅ Copia completada exitosamente.\n")
	fmt.Printf("   📄 Archivos/Carpetas copiados: %d\n", copiedCount)
	if skippedCount > 0 {
		fmt.Printf("   ⚠️  Archivos/Carpetas omitidos por falta de permisos: %d\n", skippedCount)
	}
}

// copyFile - Copiar un archivo (crear nuevo inodo y copiar bloques)
func copyFile(file *os.File, superblock *structs.SuperBloque, sourceInode *structs.Inodos) (int64, error) {
	// Buscar un inodo libre
	newInodeNum, err := findFreeInode(file, superblock)
	if err != nil {
		return -1, err
	}

	// Marcar el inodo como usado
	if err := markInodeAsUsed(file, superblock, newInodeNum); err != nil {
		return -1, err
	}
	superblock.S_free_inodes_count--

	// Crear el nuevo inodo (copia de los metadatos)
	var newInode structs.Inodos
	newInode.I_uid = sourceInode.I_uid
	newInode.I_gid = sourceInode.I_gid
	newInode.I_s = sourceInode.I_s
	newInode.I_atime = time.Now().Unix()
	newInode.I_ctime = time.Now().Unix()
	newInode.I_mtime = sourceInode.I_mtime
	newInode.I_type = sourceInode.I_type
	newInode.I_perm = sourceInode.I_perm

	// Inicializar los bloques
	for i := 0; i < 15; i++ {
		newInode.I_block[i] = -1
	}

	// Copiar los bloques de datos
	for i := 0; i < 15 && sourceInode.I_block[i] != -1; i++ {
		// Buscar un bloque libre
		newBlockNum, err := findFreeBlock(file, superblock)
		if err != nil {
			return -1, err
		}

		// Marcar el bloque como usado
		if err := markBlockAsUsed(file, superblock, newBlockNum); err != nil {
			return -1, err
		}
		superblock.S_free_blocks_count--

		// Leer el bloque de origen
		sourceBlockPos := superblock.S_block_start + (sourceInode.I_block[i] * superblock.S_block_s)
		file.Seek(sourceBlockPos, 0)

		var fileBlock structs.BloqueArchivo
		if err := binary.Read(file, binary.LittleEndian, &fileBlock); err != nil {
			return -1, err
		}

		// Escribir el bloque en la nueva posición
		newBlockPos := superblock.S_block_start + (newBlockNum * superblock.S_block_s)
		file.Seek(newBlockPos, 0)
		if err := binary.Write(file, binary.LittleEndian, &fileBlock); err != nil {
			return -1, err
		}

		// Asignar el bloque al nuevo inodo
		newInode.I_block[i] = newBlockNum
	}

	// Escribir el nuevo inodo
	newInodePos := superblock.S_inode_start + (newInodeNum * superblock.S_inode_s)
	file.Seek(newInodePos, 0)
	if err := binary.Write(file, binary.LittleEndian, &newInode); err != nil {
		return -1, err
	}

	return newInodeNum, nil
}

// copyDirectoryRecursive - Copiar un directorio recursivamente
func copyDirectoryRecursive(file *os.File, superblock *structs.SuperBloque, sourceInodeNum int64, destParentInodeNum int64, _ string, username string, groupname string, copiedCount *int, skippedCount *int) (int64, error) {
	// Leer el inodo de origen
	var sourceInode structs.Inodos
	sourceInodePos := superblock.S_inode_start + (sourceInodeNum * superblock.S_inode_s)
	file.Seek(sourceInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &sourceInode); err != nil {
		return -1, err
	}

	// Buscar un inodo libre para el nuevo directorio
	newDirInodeNum, err := findFreeInode(file, superblock)
	if err != nil {
		return -1, err
	}

	// Marcar el inodo como usado
	if err := markInodeAsUsed(file, superblock, newDirInodeNum); err != nil {
		return -1, err
	}
	superblock.S_free_inodes_count--

	// Crear el nuevo inodo del directorio
	var newDirInode structs.Inodos
	newDirInode.I_uid = sourceInode.I_uid
	newDirInode.I_gid = sourceInode.I_gid
	newDirInode.I_s = 0
	newDirInode.I_atime = time.Now().Unix()
	newDirInode.I_ctime = time.Now().Unix()
	newDirInode.I_mtime = time.Now().Unix()
	newDirInode.I_type = '0' // Directorio
	newDirInode.I_perm = sourceInode.I_perm

	// Inicializar bloques
	for i := 0; i < 15; i++ {
		newDirInode.I_block[i] = -1
	}

	// Crear el bloque inicial del directorio con . y ..
	newBlockNum, err := findFreeBlock(file, superblock)
	if err != nil {
		return -1, err
	}

	if err := markBlockAsUsed(file, superblock, newBlockNum); err != nil {
		return -1, err
	}
	superblock.S_free_blocks_count--

	newDirInode.I_block[0] = newBlockNum

	// Crear el bloque de carpeta con . y ..
	var folderBlock structs.BloqueCarpeta
	for i := 0; i < 4; i++ {
		folderBlock.BContent[i].BInodo = -1
	}

	// Entrada .
	copy(folderBlock.BContent[0].BName[:], ".")
	folderBlock.BContent[0].BInodo = newDirInodeNum

	// Entrada ..
	copy(folderBlock.BContent[1].BName[:], "..")
	folderBlock.BContent[1].BInodo = destParentInodeNum

	// Escribir el bloque
	blockPos := superblock.S_block_start + (newBlockNum * superblock.S_block_s)
	file.Seek(blockPos, 0)
	if err := binary.Write(file, binary.LittleEndian, &folderBlock); err != nil {
		return -1, err
	}

	// Escribir el nuevo inodo del directorio
	newDirInodePos := superblock.S_inode_start + (newDirInodeNum * superblock.S_inode_s)
	file.Seek(newDirInodePos, 0)
	if err := binary.Write(file, binary.LittleEndian, &newDirInode); err != nil {
		return -1, err
	}

	*copiedCount++

	// Copiar el contenido del directorio
	for i := 0; i < 15 && sourceInode.I_block[i] != -1; i++ {
		blockPos := superblock.S_block_start + (sourceInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var sourceFolderBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &sourceFolderBlock); err != nil {
			continue
		}

		// Recorrer cada entrada
		for j := 0; j < 4; j++ {
			entryName := strings.TrimRight(string(sourceFolderBlock.BContent[j].BName[:]), "\x00")
			entryName = strings.TrimSpace(entryName)

			// Saltar . y ..
			if entryName == "" || entryName == "." || entryName == ".." {
				continue
			}

			entryInodeNum := sourceFolderBlock.BContent[j].BInodo
			if entryInodeNum == -1 {
				continue
			}

			// Leer el inodo de la entrada
			var entryInode structs.Inodos
			entryInodePos := superblock.S_inode_start + (entryInodeNum * superblock.S_inode_s)
			file.Seek(entryInodePos, 0)
			if err := binary.Read(file, binary.LittleEndian, &entryInode); err != nil {
				continue
			}

			// Verificar permisos de lectura
			if !checkReadPermissionOnInode(&entryInode, username, groupname) {
				fmt.Printf("   ⚠️  Omitido '%s' (sin permisos de lectura)\n", entryName)
				*skippedCount++
				continue
			}

			// Copiar según el tipo
			if entryInode.I_type == '0' { // Directorio
				newSubDirInodeNum, err := copyDirectoryRecursive(file, superblock, entryInodeNum, newDirInodeNum, entryName, username, groupname, copiedCount, skippedCount)
				if err != nil {
					fmt.Printf("   ⚠️  Error al copiar directorio '%s': %v\n", entryName, err)
					*skippedCount++
					continue
				}

				// Agregar entrada al nuevo directorio
				if err := addEntryToDirectory(file, superblock, newDirInodeNum, entryName, newSubDirInodeNum); err != nil {
					fmt.Printf("   ⚠️  Error al agregar entrada '%s': %v\n", entryName, err)
					continue
				}

			} else { // Archivo
				newFileInodeNum, err := copyFile(file, superblock, &entryInode)
				if err != nil {
					fmt.Printf("   ⚠️  Error al copiar archivo '%s': %v\n", entryName, err)
					*skippedCount++
					continue
				}
				*copiedCount++

				// Agregar entrada al nuevo directorio
				if err := addEntryToDirectory(file, superblock, newDirInodeNum, entryName, newFileInodeNum); err != nil {
					fmt.Printf("   ⚠️  Error al agregar entrada '%s': %v\n", entryName, err)
					continue
				}
			}
		}
	}

	return newDirInodeNum, nil
}

// checkReadPermissionOnInode - Verificar permisos de lectura
func checkReadPermissionOnInode(inode *structs.Inodos, username string, groupname string) bool {
	// Root siempre tiene permisos
	if username == "root" || username == "__auto__" {
		return true
	}

	// Extraer permisos
	permsStr := string(inode.I_perm[:])
	permsStr = strings.TrimRight(permsStr, "\x00")

	if len(permsStr) < 3 {
		return false
	}

	ownerPerms := int(permsStr[0] - '0')
	groupPerms := int(permsStr[1] - '0')
	othersPerms := int(permsStr[2] - '0')

	inodeUID := fmt.Sprintf("%d", inode.I_uid)
	inodeGID := fmt.Sprintf("%d", inode.I_gid)

	// Usuario propietario - bit de lectura (4)
	if username == inodeUID {
		return (ownerPerms & 4) != 0
	}

	// Grupo - bit de lectura (4)
	if groupname == inodeGID {
		return (groupPerms & 4) != 0
	}

	// Otros - bit de lectura (4)
	return (othersPerms & 4) != 0
}

// findItemByPath - Buscar un item por su ruta completa
func findItemByPath(file *os.File, superblock *structs.SuperBloque, parsedPath *ParsedPath) (int64, error) {
	currentInodeNum := int64(0) // Raíz

	// Navegar por los directorios
	for _, dirName := range parsedPath.Directories {
		nextInode, err := findInodeInDirectory(file, superblock, currentInodeNum, dirName)
		if err != nil {
			return -1, err
		}
		currentInodeNum = nextInode
	}

	// Buscar el archivo/carpeta final
	targetInodeNum, err := findInodeInDirectory(file, superblock, currentInodeNum, parsedPath.FileName)
	if err != nil {
		return -1, err
	}

	return targetInodeNum, nil
}
