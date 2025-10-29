package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ExecuteChmod - Cambiar los permisos de un archivo o carpeta
func ExecuteChmod(path string, recursive bool, ugo string) {
	// Validar parámetros obligatorios
	if path == "" {
		fmt.Println("Error: el parámetro -path es obligatorio.")
		return
	}

	if ugo == "" {
		fmt.Println("Error: el parámetro -ugo es obligatorio.")
		return
	}

	// Validar formato de permisos (3 dígitos del 0-7)
	if len(ugo) != 3 {
		fmt.Println("Error: el parámetro -ugo debe tener exactamente 3 dígitos (ejemplo: 764).")
		return
	}

	// Validar que cada dígito esté en el rango 0-7
	for i, char := range ugo {
		digit := int(char - '0')
		if digit < 0 || digit > 7 {
			fmt.Printf("Error: el dígito %d del parámetro -ugo debe estar entre 0 y 7 (valor recibido: %c).\n", i+1, char)
			return
		}
	}

	// Validar que hay una sesión activa
	session := GetCurrentSession()
	if session == nil {
		fmt.Println("Error: no hay ninguna sesión activa. Use el comando 'login' primero.")
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

	// Obtener superbloque
	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		fmt.Printf("Error al obtener superbloque: %v\n", err)
		return
	}

	// Determinar el inodo del archivo/carpeta
	var targetInodeNum int64

	if path == "/" {
		targetInodeNum = 0
	} else {
		parsedPath := parsePath(path)
		if parsedPath == nil {
			fmt.Printf("Error: ruta inválida '%s'.\n", path)
			return
		}

		targetInodeNum, err = findItemByPath(file, superblock, parsedPath)
		if err != nil {
			fmt.Printf("Error: no se encontró la ruta '%s': %v\n", path, err)
			return
		}
	}

	// Leer el inodo del archivo/carpeta
	var targetInode structs.Inodos
	targetInodePos := superblock.S_inode_start + (targetInodeNum * superblock.S_inode_s)
	file.Seek(targetInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &targetInode); err != nil {
		fmt.Printf("Error al leer el inodo: %v\n", err)
		return
	}

	// Verificar permisos: solo root o el propietario pueden cambiar permisos
	if !session.IsRoot {
		currentUID, err := getUserIDByName(file, superblock, session.User)
		if err != nil || currentUID != targetInode.I_uid {
			fmt.Printf("Error: solo el propietario o root pueden cambiar los permisos de '%s'.\n", path)
			return
		}
	}

	// Cambiar los permisos
	changedCount := 0
	if recursive && targetInode.I_type == '0' {
		// Cambiar recursivamente
		fmt.Printf("🔐 Cambiando permisos de '%s' recursivamente a %s...\n", path, ugo)
		currentUser := session.User
		if session.IsRoot {
			currentUser = "root"
		}
		chmodRecursive(file, superblock, targetInodeNum, ugo, currentUser, &changedCount)
	} else {
		// Cambiar solo el archivo/carpeta especificado
		fmt.Printf("🔐 Cambiando permisos de '%s' a %s...\n", path, ugo)

		// Actualizar permisos
		copy(targetInode.I_perm[:], ugo)

		// Escribir el inodo actualizado
		file.Seek(targetInodePos, 0)
		if err := binary.Write(file, binary.LittleEndian, &targetInode); err != nil {
			fmt.Printf("Error al actualizar el inodo: %v\n", err)
			return
		}
		changedCount = 1
	}

	// Actualizar el superbloque
	file.Seek(superblock.S_inode_start-int64(binary.Size(structs.SuperBloque{})), 0)
	if err := binary.Write(file, binary.LittleEndian, superblock); err != nil {
		fmt.Printf("Error al actualizar el superbloque: %v\n", err)
		return
	}

	fmt.Printf("✅ Permisos cambiados exitosamente.\n")
	fmt.Printf("   📄 Archivos/Carpetas modificados: %d\n", changedCount)
}

// chmodRecursive - Cambiar permisos recursivamente
func chmodRecursive(file *os.File, superblock *structs.SuperBloque, dirInodeNum int64, permissions string, currentUser string, changedCount *int) {
	// Leer el inodo del directorio
	var dirInode structs.Inodos
	dirInodePos := superblock.S_inode_start + (dirInodeNum * superblock.S_inode_s)
	file.Seek(dirInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &dirInode); err != nil {
		return
	}

	// Verificar si el archivo/carpeta pertenece al usuario actual (si no es root)
	shouldChange := currentUser == "root"
	if !shouldChange {
		currentUID, err := getUserIDByName(file, superblock, currentUser)
		if err == nil && currentUID == dirInode.I_uid {
			shouldChange = true
		}
	}

	// Cambiar los permisos del directorio actual si corresponde
	if shouldChange {
		copy(dirInode.I_perm[:], permissions)
		file.Seek(dirInodePos, 0)
		if err := binary.Write(file, binary.LittleEndian, &dirInode); err != nil {
			return
		}
		*changedCount++
	}

	// Si no es un directorio, terminar
	if dirInode.I_type != '0' {
		return
	}

	// Recorrer los bloques del directorio
	for i := 0; i < 15 && dirInode.I_block[i] != -1; i++ {
		blockPos := superblock.S_block_start + (dirInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var folderBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &folderBlock); err != nil {
			continue
		}

		// Recorrer cada entrada
		for j := 0; j < 4; j++ {
			entryName := strings.TrimRight(string(folderBlock.BContent[j].BName[:]), "\x00")
			entryName = strings.TrimSpace(entryName)

			// Saltar entradas vacías, . y ..
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

			// Si es un directorio, procesar recursivamente
			if entryInode.I_type == '0' {
				chmodRecursive(file, superblock, entryInodeNum, permissions, currentUser, changedCount)
			} else {
				// Si es un archivo, verificar propiedad y cambiar permisos
				shouldChangeFile := currentUser == "root"
				if !shouldChangeFile {
					currentUID, err := getUserIDByName(file, superblock, currentUser)
					if err == nil && currentUID == entryInode.I_uid {
						shouldChangeFile = true
					}
				}

				if shouldChangeFile {
					copy(entryInode.I_perm[:], permissions)
					file.Seek(entryInodePos, 0)
					if err := binary.Write(file, binary.LittleEndian, &entryInode); err != nil {
						continue
					}
					*changedCount++
				}
			}
		}
	}
}

// getUserIDByName - Obtener el UID de un usuario por su nombre
func getUserIDByName(file *os.File, superblock *structs.SuperBloque, username string) (int64, error) {
	// Buscar en el archivo users.txt (inodo 1)
	usersInodeNum := int64(1)

	var usersInode structs.Inodos
	usersInodePos := superblock.S_inode_start + (usersInodeNum * superblock.S_inode_s)
	file.Seek(usersInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &usersInode); err != nil {
		return -1, fmt.Errorf("no se pudo leer el archivo de usuarios")
	}

	// Leer el contenido del archivo users.txt
	var content strings.Builder
	for i := 0; i < 15 && usersInode.I_block[i] != -1; i++ {
		blockPos := superblock.S_block_start + (usersInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var fileBlock structs.BloqueArchivo
		if err := binary.Read(file, binary.LittleEndian, &fileBlock); err != nil {
			continue
		}

		blockContent := string(fileBlock.BContent[:])
		blockContent = strings.TrimRight(blockContent, "\x00")
		content.WriteString(blockContent)
	}

	// Parsear el contenido
	lines := strings.Split(content.String(), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Formato: UID,tipo,grupo,usuario,contraseña
		parts := strings.Split(line, ",")
		if len(parts) >= 4 {
			uid := strings.TrimSpace(parts[0])
			user := strings.TrimSpace(parts[3])

			if user == username {
				uidNum, err := strconv.ParseInt(uid, 10, 64)
				if err != nil {
					return -1, fmt.Errorf("UID inválido para el usuario %s", username)
				}
				return uidNum, nil
			}
		}
	}

	return -1, fmt.Errorf("usuario no encontrado")
}
