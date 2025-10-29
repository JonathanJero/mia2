package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func ExecuteRmgrp(groupName string) {
	// Verificar sesión activa
	if !RequireActiveSession() {
		return
	}

	// Validar parámetros
	if groupName == "" {
		fmt.Println("Error: el parámetro -name es obligatorio para rmgrp.")
		return
	}

	// Obtener sesión actual
	session := GetCurrentSession()
	if session == nil {
		fmt.Println("Error: No se pudo obtener la sesión actual.")
		return
	}

	// Verificar que solo root puede eliminar grupos
	if !session.IsRoot {
		fmt.Printf("Error: Solo el usuario 'root' puede eliminar grupos. Usuario actual: '%s'\n", session.User)
		return
	}

	// Verificar que no se está intentando eliminar el grupo root
	if groupName == "root" {
		fmt.Println("Error: No se puede eliminar el grupo 'root'.")
		return
	}

	// Buscar la partición montada de la sesión
	mounted := GetMountedPartition(session.PartitionID)
	if mounted == nil {
		fmt.Printf("Error: No se encontró la partición montada con ID '%s'.\n", session.PartitionID)
		return
	}

	// Eliminar el grupo del archivo users.txt
	err := removeGroupFromUsersFile(mounted, groupName)
	if err != nil {
		fmt.Printf("Error al eliminar el grupo '%s': %v\n", groupName, err)
		return
	}

	fmt.Printf("Grupo '%s' eliminado exitosamente.\n", groupName)
}

// Eliminar grupo del archivo users.txt
func removeGroupFromUsersFile(mounted *MountedPartition, groupName string) error {
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

	// Verificar que el grupo existe y marcarlo como eliminado
	newContent, found, err := markGroupAsDeleted(currentContent, groupName)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("el grupo '%s' no existe", groupName)
	}

	// Escribir el contenido actualizado
	err = WriteUsersFileContent(mounted, newContent)
	if err != nil {
		return fmt.Errorf("error al escribir users.txt: %v", err)
	}

	fmt.Printf("Grupo '%s' marcado como eliminado (GID=0).\n", groupName)
	return nil
}

// Marcar grupo como eliminado (cambiar GID a 0)
func markGroupAsDeleted(content, groupName string) (string, bool, error) {
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
		if len(parts) >= 3 {
			gidStr := strings.TrimSpace(parts[0])
			tipo := strings.TrimSpace(parts[1])
			name := strings.TrimSpace(parts[2])

			// Convertir GID a entero para validar
			gid, err := strconv.Atoi(gidStr)
			if err != nil {
				newLines = append(newLines, originalLine)
				continue
			}

			// Si es el grupo que buscamos y no está ya eliminado
			if tipo == "G" && name == groupName {
				if gid == 0 {
					return "", false, fmt.Errorf("el grupo '%s' ya está eliminado", groupName)
				}

				// Marcar como eliminado (GID = 0)
				newLine := fmt.Sprintf("0,G,%s", name)
				newLines = append(newLines, newLine)
				found = true

				fmt.Printf("Grupo encontrado y marcado para eliminación: %s -> %s\n", originalLine, newLine)
			} else {
				newLines = append(newLines, originalLine)
			}
		} else {
			newLines = append(newLines, originalLine)
		}
	}

	return strings.Join(newLines, "\n"), found, nil
}
