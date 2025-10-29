package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"
)

// ExecuteRename - Cambiar el nombre de un archivo o carpeta
func ExecuteRename(path string, name string) {
	// Validar parámetros obligatorios
	if path == "" {
		fmt.Println("Error: el parámetro -path es obligatorio.")
		return
	}

	if name == "" {
		fmt.Println("Error: el parámetro -name es obligatorio.")
		return
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

	// Validar el nuevo nombre
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") {
		fmt.Printf("Error: el nombre '%s' es inválido. No debe contener '/'.\n", name)
		return
	}

	// Validar que el nuevo nombre no sea . o ..
	if name == "." || name == ".." {
		fmt.Printf("Error: no se puede renombrar a '%s'.\n", name)
		return
	}

	// Validar longitud del nombre (máximo 12 caracteres según BContent)
	if len(name) > 12 {
		fmt.Printf("Error: el nombre es demasiado largo (máximo 12 caracteres). Tamaño actual: %d.\n", len(name))
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

	// Buscar el directorio padre navegando por los directorios
	parentInodeNum := int64(0) // Inodo raíz

	// Navegar por los directorios
	for _, dirName := range parsedPath.Directories {
		nextInode, err := findInodeInDirectory(file, superblock, parentInodeNum, dirName)
		if err != nil {
			fmt.Printf("Error: no se encontró el directorio '%s': %v\n", dirName, err)
			return
		}
		parentInodeNum = nextInode
	}

	// Buscar el archivo/carpeta objetivo en el directorio padre
	targetInodeNum, err := findInodeInDirectory(file, superblock, parentInodeNum, parsedPath.FileName)
	if err != nil {
		// Intentar búsqueda tolerante: si el nombre es mayor a 12 caracteres, probar su versión truncada
		truncated := parsedPath.FileName
		if len(truncated) > 12 {
			truncated = truncated[:12]
			altInode, altErr := findInodeInDirectory(file, superblock, parentInodeNum, truncated)
			if altErr == nil {
				fmt.Printf("⚠️  Nombre '%s' no encontrado, usando versión truncada '%s' para la operación.\n", parsedPath.FileName, truncated)
				parsedPath.FileName = truncated
				targetInodeNum = altInode
				err = nil
			}
		}
		if err != nil {
			fmt.Printf("Error: no se encontró '%s': %v\n", parsedPath.FileName, err)
			return
		}
	}

	// Leer el inodo del objetivo
	var targetInode structs.Inodos
	targetInodePos := superblock.S_inode_start + (targetInodeNum * superblock.S_inode_s)
	file.Seek(targetInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &targetInode); err != nil {
		fmt.Printf("Error al leer el inodo: %v\n", err)
		return
	}

	// Validar permisos de escritura sobre el archivo/carpeta
	if !checkWritePermissionOnInode(&targetInode, session.User, session.Group) {
		fmt.Printf("Error: no tiene permisos de escritura sobre '%s'.\n", path)
		return
	}

	// Leer el inodo del directorio padre
	var parentInode structs.Inodos
	parentInodePos := superblock.S_inode_start + (parentInodeNum * superblock.S_inode_s)
	file.Seek(parentInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &parentInode); err != nil {
		fmt.Printf("Error al leer el inodo del directorio padre: %v\n", err)
		return
	}

	// Verificar que no existe otro archivo/carpeta con el nuevo nombre en el mismo directorio
	existingInode, _ := findInodeInDirectory(file, superblock, parentInodeNum, name)
	if existingInode != -1 {
		fmt.Printf("Error: ya existe un archivo o carpeta con el nombre '%s' en el mismo directorio.\n", name)
		return
	}

	// Buscar y actualizar la entrada en el directorio padre
	renamed := false
	for i := 0; i < 15 && parentInode.I_block[i] != -1 && !renamed; i++ {
		blockPos := superblock.S_block_start + (parentInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var folderBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &folderBlock); err != nil {
			continue
		}

		// Buscar la entrada con el nombre actual
		for j := 0; j < 4; j++ {
			entryName := strings.TrimRight(string(folderBlock.BContent[j].BName[:]), "\x00")
			entryName = strings.TrimSpace(entryName)

			// Verificar si es la entrada que buscamos
			if entryName == parsedPath.FileName && folderBlock.BContent[j].BInodo == targetInodeNum {
				// Actualizar el nombre
				for k := range folderBlock.BContent[j].BName {
					folderBlock.BContent[j].BName[k] = 0
				}
				copy(folderBlock.BContent[j].BName[:], name)

				// Escribir el bloque actualizado
				file.Seek(blockPos, 0)
				if err := binary.Write(file, binary.LittleEndian, &folderBlock); err != nil {
					fmt.Printf("Error al actualizar el directorio: %v\n", err)
					return
				}

				renamed = true
				break
			}
		}
	}

	if !renamed {
		fmt.Printf("Error: no se pudo actualizar el nombre del archivo.\n")
		return
	}

	// Actualizar el tiempo de modificación del directorio padre
	parentInode.I_mtime = time.Now().Unix()
	file.Seek(parentInodePos, 0)
	if err := binary.Write(file, binary.LittleEndian, &parentInode); err != nil {
		fmt.Printf("Error al actualizar el directorio padre: %v\n", err)
		return
	}

	// Determinar si es archivo o carpeta
	itemType := "archivo"
	if targetInode.I_type == '0' {
		itemType = "carpeta"
	}

	fmt.Printf("✅ El %s '%s' fue renombrado a '%s' exitosamente.\n", itemType, parsedPath.FileName, name)
}
