package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"
)

// ExecuteRemove - Eliminar archivo o carpeta con validación de permisos
func ExecuteRemove(path string) {
	// Validar parámetro obligatorio
	if path == "" {
		fmt.Println("Error: el parámetro -path es obligatorio.")
		return
	}

	// Validar que hay una sesión activa usando la función de session.go
	session := GetCurrentSession()
	if session == nil {
		fmt.Println("Error: no hay ninguna sesión activa. Use el comando 'login' primero.")
		return
	}

	// Validar que la ruta no sea raíz
	if path == "/" {
		fmt.Println("Error: no se puede eliminar la raíz del sistema de archivos.")
		return
	}

	// Normalizar la ruta
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
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

	// Obtener superbloque usando función existente
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

	// Buscar el directorio padre y el archivo/carpeta objetivo
	parentInodeNum := int64(0) // Raíz por defecto

	// Navegar por los directorios
	for _, dirName := range parsedPath.Directories {
		nextInode, err := findInodeInDirectory(file, superblock, parentInodeNum, dirName)
		if err != nil {
			fmt.Printf("Error: no se encontró el directorio '%s': %v\n", dirName, err)
			return
		}
		parentInodeNum = nextInode
	}

	// Buscar el archivo/carpeta objetivo
	targetInodeNum, err := findInodeInDirectory(file, superblock, parentInodeNum, parsedPath.FileName)
	if err != nil {
		fmt.Printf("Error: no se encontró '%s': %v\n", parsedPath.FileName, err)
		return
	}

	// Leer el inodo del directorio padre
	var parentInode structs.Inodos
	parentInodePos := superblock.S_inode_start + (parentInodeNum * superblock.S_inode_s)
	file.Seek(parentInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &parentInode); err != nil {
		fmt.Printf("Error al leer el inodo padre: %v\n", err)
		return
	}

	// Validar permisos de escritura en el directorio padre
	if !checkWritePermissionOnInode(&parentInode, session.User, session.Group) {
		fmt.Printf("Error: no tiene permisos de escritura en el directorio padre.\n")
		return
	}

	// Leer el inodo del objetivo
	var targetInode structs.Inodos
	targetInodePos := superblock.S_inode_start + (targetInodeNum * superblock.S_inode_s)
	file.Seek(targetInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &targetInode); err != nil {
		fmt.Printf("Error al leer el inodo objetivo: %v\n", err)
		return
	}

	// Validar permisos de escritura sobre el archivo/carpeta
	if !checkWritePermissionOnInode(&targetInode, session.User, session.Group) {
		fmt.Printf("Error: no tiene permisos de escritura sobre '%s'.\n", path)
		return
	}

	// Si es un directorio, intentar eliminarlo recursivamente
	if targetInode.I_type == '0' { // Directorio
		fmt.Printf("🗂️  Eliminando directorio '%s' y su contenido...\n", path)

		// Validar que se pueden eliminar todos los archivos dentro
		canDelete, failedPath := canDeleteDirectoryRecursive(file, superblock, &targetInode, session.User, session.Group)
		if !canDelete {
			fmt.Printf("❌ Error: no se puede eliminar el directorio '%s'.\n", path)
			fmt.Printf("   No tiene permisos de escritura sobre: %s\n", failedPath)
			return
		}

		// Eliminar recursivamente
		if err := deleteDirectoryRecursiveInternal(file, superblock, targetInodeNum); err != nil {
			fmt.Printf("Error al eliminar el directorio: %v\n", err)
			return
		}
	} else { // Archivo
		fmt.Printf("📄 Eliminando archivo '%s'...\n", path)

		// Eliminar el archivo
		if err := deleteFileInternal(file, superblock, targetInodeNum); err != nil {
			fmt.Printf("Error al eliminar el archivo: %v\n", err)
			return
		}
	}

	// Eliminar la entrada del directorio padre
	if err := removeEntryFromDirectoryInternal(file, superblock, &parentInode, parsedPath.FileName, parentInodeNum); err != nil {
		fmt.Printf("Error al actualizar el directorio padre: %v\n", err)
		return
	}

	// Actualizar el superbloque
	superblock.S_free_inodes_count++
	file.Seek(superblock.S_inode_start-int64(binary.Size(structs.SuperBloque{})), 0)
	if err := binary.Write(file, binary.LittleEndian, superblock); err != nil {
		fmt.Printf("Error al actualizar el superbloque: %v\n", err)
		return
	}

	err = WriteJournal(
		mounted,
		"remove",
		path,
		"",
	)
	if err != nil {
		fmt.Printf("⚠️ No se pudo escribir al journal: %v\n", err)
	}

	fmt.Printf("✅ '%s' eliminado exitosamente.\n", path)
}

// canDeleteDirectoryRecursive - Verificar permisos recursivamente
func canDeleteDirectoryRecursive(file *os.File, superblock *structs.SuperBloque, dirInode *structs.Inodos, username string, groupname string) (bool, string) {
	// Recorrer todos los bloques del directorio
	for i := 0; i < 15 && dirInode.I_block[i] != -1; i++ {
		blockPos := superblock.S_block_start + (dirInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var folderBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &folderBlock); err != nil {
			continue
		}

		// Revisar cada entrada
		for j := 0; j < 4; j++ {
			entryName := strings.TrimRight(string(folderBlock.BContent[j].BName[:]), "\x00")
			entryName = strings.TrimSpace(entryName)

			// Saltar . y ..
			if entryName == "" || entryName == "." || entryName == ".." {
				continue
			}

			entryInodeNum := folderBlock.BContent[j].BInodo
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

			// Verificar permisos de escritura
			if !checkWritePermissionOnInode(&entryInode, username, groupname) {
				return false, entryName
			}

			// Si es un directorio, verificar recursivamente
			if entryInode.I_type == '0' {
				canDelete, failedPath := canDeleteDirectoryRecursive(file, superblock, &entryInode, username, groupname)
				if !canDelete {
					return false, entryName + "/" + failedPath
				}
			}
		}
	}

	return true, ""
}

// deleteDirectoryRecursiveInternal - Eliminar directorio y su contenido
func deleteDirectoryRecursiveInternal(file *os.File, superblock *structs.SuperBloque, inodeNum int64) error {
	// Leer el inodo del directorio
	var dirInode structs.Inodos
	inodePos := superblock.S_inode_start + (inodeNum * superblock.S_inode_s)
	file.Seek(inodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &dirInode); err != nil {
		return err
	}

	// Recorrer todos los bloques del directorio
	for i := 0; i < 15 && dirInode.I_block[i] != -1; i++ {
		blockPos := superblock.S_block_start + (dirInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var folderBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &folderBlock); err != nil {
			continue
		}

		// Eliminar cada entrada
		for j := 0; j < 4; j++ {
			entryName := strings.TrimRight(string(folderBlock.BContent[j].BName[:]), "\x00")
			entryName = strings.TrimSpace(entryName)

			// Saltar . y ..
			if entryName == "" || entryName == "." || entryName == ".." {
				continue
			}

			entryInodeNum := folderBlock.BContent[j].BInodo
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

			// Si es un directorio, eliminar recursivamente
			if entryInode.I_type == '0' {
				if err := deleteDirectoryRecursiveInternal(file, superblock, entryInodeNum); err != nil {
					return err
				}
			} else {
				// Si es un archivo, eliminarlo
				if err := deleteFileInternal(file, superblock, entryInodeNum); err != nil {
					return err
				}
			}
		}

		// Marcar el bloque como libre usando función existente
		if err := markBlockAsFree(file, superblock, dirInode.I_block[i]); err != nil {
			return err
		}
		superblock.S_free_blocks_count++
	}

	// Marcar el inodo del directorio como libre usando función existente
	if err := markInodeAsFree(file, superblock, inodeNum); err != nil {
		return err
	}

	return nil
}

// deleteFileInternal - Eliminar un archivo
func deleteFileInternal(file *os.File, superblock *structs.SuperBloque, inodeNum int64) error {
	// Leer el inodo del archivo
	var fileInode structs.Inodos
	inodePos := superblock.S_inode_start + (inodeNum * superblock.S_inode_s)
	file.Seek(inodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &fileInode); err != nil {
		return err
	}

	// Liberar todos los bloques del archivo usando función existente
	for i := 0; i < 15 && fileInode.I_block[i] != -1; i++ {
		if err := markBlockAsFree(file, superblock, fileInode.I_block[i]); err != nil {
			return err
		}
		superblock.S_free_blocks_count++
	}

	// Liberar el inodo usando función existente
	if err := markInodeAsFree(file, superblock, inodeNum); err != nil {
		return err
	}

	return nil
}

// removeEntryFromDirectoryInternal - Eliminar entrada de un directorio
func removeEntryFromDirectoryInternal(file *os.File, superblock *structs.SuperBloque, dirInode *structs.Inodos, entryName string, dirInodeNum int64) error {
	// Buscar y eliminar la entrada en los bloques del directorio
	for i := 0; i < 15 && dirInode.I_block[i] != -1; i++ {
		blockPos := superblock.S_block_start + (dirInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var folderBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &folderBlock); err != nil {
			continue
		}

		// Buscar la entrada
		for j := 0; j < 4; j++ {
			currentName := strings.TrimRight(string(folderBlock.BContent[j].BName[:]), "\x00")
			currentName = strings.TrimSpace(currentName)

			if currentName == entryName {
				// Limpiar la entrada
				folderBlock.BContent[j].BInodo = -1
				for k := range folderBlock.BContent[j].BName {
					folderBlock.BContent[j].BName[k] = 0
				}

				// Escribir el bloque actualizado
				file.Seek(blockPos, 0)
				if err := binary.Write(file, binary.LittleEndian, &folderBlock); err != nil {
					return err
				}

				// Actualizar el inodo del directorio (tiempo de modificación)
				dirInode.I_mtime = time.Now().Unix()
				inodePos := superblock.S_inode_start + (dirInodeNum * superblock.S_inode_s)
				file.Seek(inodePos, 0)
				if err := binary.Write(file, binary.LittleEndian, dirInode); err != nil {
					return err
				}

				return nil
			}
		}
	}

	return fmt.Errorf("no se encontró la entrada '%s' en el directorio", entryName)
}

// checkWritePermissionOnInode - Verificar permisos de escritura comparando con usuarios/grupos del sistema
func checkWritePermissionOnInode(inode *structs.Inodos, username string, groupname string) bool {
	// Root siempre tiene permisos
	if username == "root" || username == "__auto__" {
		return true
	}

	// Extraer permisos desde el array de bytes (formato: "664" -> [54, 54, 52])
	// Los bytes representan caracteres ASCII: '6' = 54, '4' = 52
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

	// Convertir UID del inodo a string para comparar con el nombre de usuario
	inodeUID := fmt.Sprintf("%d", inode.I_uid)
	inodeGID := fmt.Sprintf("%d", inode.I_gid)

	// Usuario propietario
	if username == inodeUID {
		return (ownerPerms & 2) != 0 // bit de escritura (2 en octal = write)
	}

	// Grupo
	if groupname == inodeGID {
		return (groupPerms & 2) != 0
	}

	// Otros
	return (othersPerms & 2) != 0
}
