package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func ExecuteRmusr(username string) {
	// Verificar sesión activa
	if !RequireActiveSession() {
		return
	}

	// Validar parámetro obligatorio
	if username == "" {
		fmt.Println("Error: el parámetro -user es obligatorio para rmusr.")
		return
	}

	// Obtener sesión actual
	session := GetCurrentSession()
	if session == nil {
		fmt.Println("Error: No se pudo obtener la sesión actual.")
		return
	}

	// Verificar que solo root puede eliminar usuarios
	if !session.IsRoot {
		fmt.Printf("Error: Solo el usuario 'root' puede eliminar usuarios. Usuario actual: '%s'\n", session.User)
		return
	}

	// Buscar la partición montada de la sesión
	mounted := GetMountedPartition(session.PartitionID)
	if mounted == nil {
		fmt.Printf("Error: No se encontró la partición montada con ID '%s'.\n", session.PartitionID)
		return
	}

	// Eliminar el usuario del archivo users.txt
	err := removeUserFromUsersFile(mounted, username)
	if err != nil {
		fmt.Printf("Error al eliminar el usuario '%s': %v\n", username, err)
		return
	}

	fmt.Printf("Usuario '%s' eliminado exitosamente.\n", username)
}

// Eliminar usuario del archivo users.txt (marcar UID como 0)
func removeUserFromUsersFile(mounted *MountedPartition, username string) error {
	// Abrir el archivo del disco
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	// Leer el MBR para obtener información de la partición
	var mbr structs.MBR
	if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
		return fmt.Errorf("error al leer el MBR: %v", err)
	}

	// Encontrar la partición específica
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
		return fmt.Errorf("no se pudo encontrar la partición '%s'", mounted.Name)
	}

	// Leer el superbloque
	file.Seek(partition.Part_start, 0)
	var superblock structs.SuperBloque
	if err := binary.Read(file, binary.LittleEndian, &superblock); err != nil {
		return fmt.Errorf("error al leer el superbloque: %v", err)
	}

	// Leer el contenido actual del archivo users.txt
	currentContent, err := ReadUsersFileContent(mounted)
	if err != nil {
		return fmt.Errorf("error al leer users.txt: %v", err)
	}

	// Marcar usuario como eliminado
	newContent, found, err := markUserAsDeleted(currentContent, username)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("el usuario '%s' no existe", username)
	}

	// Escribir el contenido actualizado
	err = WriteUsersFileContent(mounted, newContent)
	if err != nil {
		return fmt.Errorf("error al escribir users.txt: %v", err)
	}

	fmt.Printf("Usuario '%s' marcado como eliminado (UID=0).\n", username)
	return nil
}

func markUserAsDeleted(content, username string) (string, bool, error) {
	lines := strings.Split(content, "\n")
	var newLines []string
	found := false

	for _, line := range lines {
		originalLine := line
		line = strings.TrimSpace(line)
		if line == "" {
			newLines = append(newLines, originalLine)
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) >= 4 {
			uidStr := strings.TrimSpace(parts[0])
			tipo := strings.TrimSpace(parts[1])
			groupNameOrGID := strings.TrimSpace(parts[2])
			user := strings.TrimSpace(parts[3])

			uid, err := strconv.Atoi(uidStr)
			if err != nil {
				newLines = append(newLines, originalLine)
				continue
			}
			if tipo == "U" && user == username {
				if uid == 0 {
					return "", false, fmt.Errorf("el usuario '%s' ya está eliminado", username)
				}
				if len(parts) >= 5 {
					password := strings.TrimSpace(parts[4])
					newLine := fmt.Sprintf("0,U,%s,%s,%s", groupNameOrGID, user, password)
					newLines = append(newLines, newLine)
				} else {
					newLine := fmt.Sprintf("0,U,%s,%s", groupNameOrGID, user)
					newLines = append(newLines, newLine)
				}
				found = true
				fmt.Printf("Usuario encontrado y marcado para eliminación: %s -> %s\n", originalLine, newLines[len(newLines)-1])
			} else {
				newLines = append(newLines, originalLine)
			}
		} else {
			newLines = append(newLines, originalLine)
		}
	}
	return strings.Join(newLines, "\n"), found, nil
}
