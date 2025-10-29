package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// Agregar Start a MountedPartition (solo para uso en memoria)
type MountedPartition struct {
	ID    string
	Path  string
	Name  string
	Size  int64
	Start int64 // Posición de inicio de la partición
}

var mountedPartitions []MountedPartition
var diskCounters = make(map[string]int)

// NOTE: mount state is kept only in memory (no on-disk persistence)

func ExecuteMount(path string, name string) {
	if name == "" {
		fmt.Println("Error: el parámetro -name es obligatorio para mount.")
		return
	}

	// Asegurar que el archivo tiene extensión .mia
	if !strings.HasSuffix(strings.ToLower(path), ".mia") {
		path += ".mia"
	}

	// Verificar que el archivo existe
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Error: El archivo '%s' no existe.\n", path)
		return
	}

	// Abrir el archivo del disco EN MODO LECTURA/ESCRITURA
	file, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		fmt.Printf("Error al abrir el archivo: %v\n", err)
		return
	}
	defer file.Close()

	// Leer el MBR
	var mbr structs.MBR
	if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
		fmt.Printf("Error al leer el MBR: %v\n", err)
		return
	}

	// Variables para la partición encontrada
	var foundPartition *structs.Partition
	var partitionIndex int = -1
	var partitionStart int64
	var partitionSize int64
	isLogical := false

	// PASO 1: Buscar en particiones primarias
	for i, partition := range mbr.Mbr_partitions {
		if partition.Part_status != '0' && partition.Part_s > 0 {
			partitionNameBytes := partition.Part_name[:]
			nullIndex := len(partitionNameBytes)
			for j, b := range partitionNameBytes {
				if b == 0 {
					nullIndex = j
					break
				}
			}
			partitionName := strings.TrimSpace(string(partitionNameBytes[:nullIndex]))

			if strings.EqualFold(partitionName, name) || partitionName == name {
				// Solo montar particiones primarias (P o p)
				if partition.Part_type == 'P' || partition.Part_type == 'p' {
					partitionCopy := partition
					foundPartition = &partitionCopy
					partitionIndex = i
					partitionStart = partition.Part_start
					partitionSize = partition.Part_s
					break
				} else if partition.Part_type == 'E' || partition.Part_type == 'e' {
					fmt.Printf("Error: No se pueden montar particiones extendidas. La partición '%s' es de tipo extendida.\n", name)
					return
				}
			}
		}
	}

	// PASO 2: Si no se encontró en primarias, buscar en particiones lógicas
	if foundPartition == nil {
		fmt.Printf("🔍 Buscando '%s' en particiones lógicas...\n", name)

		// Encontrar la partición extendida
		var extendedPartition *structs.Partition
		for i := 0; i < 4; i++ {
			if mbr.Mbr_partitions[i].Part_s > 0 &&
				(mbr.Mbr_partitions[i].Part_type == 'E' || mbr.Mbr_partitions[i].Part_type == 'e') {
				extendedPartition = &mbr.Mbr_partitions[i]
				break
			}
		}

		if extendedPartition != nil {
			// Recorrer la cadena de EBRs
			currentEBRPos := extendedPartition.Part_start

			for currentEBRPos != -1 {
				file.Seek(currentEBRPos, 0)
				var ebr structs.EBR
				if err := binary.Read(file, binary.LittleEndian, &ebr); err != nil {
					break
				}

				// Si el EBR está vacío, terminar
				if ebr.PartS == 0 {
					break
				}

				// Extraer el nombre de la partición lógica
				ebrNameBytes := ebr.PartName[:]
				nullIndex := len(ebrNameBytes)
				for j, b := range ebrNameBytes {
					if b == 0 {
						nullIndex = j
						break
					}
				}
				ebrName := strings.TrimSpace(string(ebrNameBytes[:nullIndex]))

				// Comparar nombres
				if strings.EqualFold(ebrName, name) || ebrName == name {
					fmt.Printf("✅ Partición lógica '%s' encontrada en EBR\n", name)
					partitionStart = ebr.PartStart
					partitionSize = ebr.PartS
					isLogical = true
					break
				}

				// Siguiente EBR en la cadena
				currentEBRPos = ebr.PartNext
			}
		}

		// Si aún no se encontró, mostrar error
		if !isLogical {
			fmt.Printf("Error: No se encontró la partición '%s' en el disco '%s'.\n", name, path)
			return
		}
	}

	// PASO 3: Verificar si la partición ya está montada
	for _, mounted := range mountedPartitions {
		if mounted.Path == path && mounted.Name == name {
			fmt.Printf("Error: La partición '%s' del disco '%s' ya está montada con ID '%s'.\n", name, path, mounted.ID)
			return
		}
	}

	// PASO 4: Generar el ID y correlativo (SOLO EN MEMORIA)
	id := generatePartitionID(path)
	correlativo := generateCorrelativo()

	// PASO 5: Actualizar SOLO el MBR si es partición primaria
	// (NO tocamos los EBRs, solo guardamos la info en memoria)
	if !isLogical && partitionIndex != -1 {
		// Actualizar la partición primaria en el MBR
		mbr.Mbr_partitions[partitionIndex].Part_correlative = int64(correlativo)
		copy(mbr.Mbr_partitions[partitionIndex].Part_id[:], []byte(id)[:4])

		// Escribir el MBR actualizado
		file.Seek(0, 0)
		if err := binary.Write(file, binary.LittleEndian, &mbr); err != nil {
			fmt.Printf("Error al actualizar el MBR: %v\n", err)
			return
		}
	}

	// PASO 6: Crear la entrada en memoria (con o sin actualizar disco)
	mountedPartition := MountedPartition{
		ID:    id,
		Path:  path,
		Name:  name,
		Size:  partitionSize,
		Start: partitionStart, // ← Guardar posición de inicio
	}

	// Agregar a la lista de particiones montadas (memoria solamente)
	mountedPartitions = append(mountedPartitions, mountedPartition)

	// PASO 7: Mostrar mensaje de éxito
	partitionType := "Primaria"
	if isLogical {
		partitionType = "Lógica"
	}

	fmt.Printf("Partición '%s' (%s) montada exitosamente.\n", name, partitionType)
	fmt.Printf("   Disco: %s\n", path)
	fmt.Printf("   ID asignado: %s\n", id)
	fmt.Printf("   Correlativo: %d\n", correlativo)
	fmt.Printf("   Tamaño: %d bytes\n", partitionSize)
}

// Generar correlativo secuencial
func generateCorrelativo() int {
	return len(mountedPartitions) + 1
}

func generatePartitionID(diskPath string) string {
	// Últimos dos dígitos del carnet: 202300850 -> 50
	// Cambiado por petición: usar sufijo 53 en lugar de 50
	carnetSuffix := "53"

	// Verificar si es el mismo disco o uno nuevo
	partitionNumber, exists := diskCounters[diskPath]
	if !exists {
		// Es un disco nuevo, usar la siguiente letra
		partitionNumber = 1
		diskCounters[diskPath] = partitionNumber
	} else {
		// Es el mismo disco, incrementar el número de partición
		partitionNumber++
		diskCounters[diskPath] = partitionNumber
	}

	// Determinar la letra según el número de discos diferentes montados
	currentLetter := getLetter()

	// Formato: últimos 2 dígitos + número de partición + letra
	return fmt.Sprintf("%s%d%c", carnetSuffix, partitionNumber, currentLetter)
}

func getLetter() byte {
	// Si es el primer disco de esta "serie", usar A
	// Si ya hay discos montados, verificar si necesitamos nueva letra
	uniqueDisks := len(diskCounters)

	if uniqueDisks == 1 {
		return 'A'
	}

	// Para múltiples discos, usar letras consecutivas
	letters := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if uniqueDisks-1 < len(letters) {
		return letters[uniqueDisks-1]
	}

	return 'Z' // Fallback si se excede el alfabeto
}

func ExecuteMounted() {
	if len(mountedPartitions) == 0 {
		fmt.Println("No hay particiones montadas.")
		return
	}

	fmt.Println("Particiones montadas:")
	for _, mounted := range mountedPartitions {
		fmt.Printf("ID: %s\n", mounted.ID)
		fmt.Printf("Nombre: %s\n", mounted.Name)
		fmt.Printf("Ruta: %s\n", mounted.Path)
		fmt.Println("-------------------------")
	}
}

func ExecuteUnmount(id string) {
	if id == "" {
		fmt.Println("Error: el parámetro -id es obligatorio para unmount.")
		return
	}

	for i, mounted := range mountedPartitions {
		if strings.EqualFold(mounted.ID, id) {
			// Abrir el archivo del disco en modo lectura/escritura
			file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
			if err != nil {
				fmt.Printf("Error al abrir el archivo del disco: %v\n", err)
				return
			}
			defer file.Close()

			// Leer el MBR
			var mbr structs.MBR
			if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
				fmt.Printf("Error al leer el MBR: %v\n", err)
				return
			}

			// Buscar la partición por ID y limpiar su ID y correlativo
			for j, partition := range mbr.Mbr_partitions {
				partID := strings.TrimSpace(string(partition.Part_id[:]))
				// Comparación insensible a mayúsculas/minúsculas para tolerar IDs como '531A' vs '531a'
				if strings.EqualFold(partID, id) {
					// Limpiar ID y correlativo
					for k := range mbr.Mbr_partitions[j].Part_id {
						mbr.Mbr_partitions[j].Part_id[k] = 0
					}
					mbr.Mbr_partitions[j].Part_correlative = 0

					// Escribir el MBR actualizado de vuelta al disco
					file.Seek(0, 0)
					if err := binary.Write(file, binary.LittleEndian, &mbr); err != nil {
						fmt.Printf("Error al actualizar el MBR: %v\n", err)
						return
					}

					// Remover de la lista de particiones montadas (memoria solamente)
					mountedPartitions = append(mountedPartitions[:i], mountedPartitions[i+1:]...)
					fmt.Printf("Partición con ID '%s' desmontada exitosamente.\n", id)
					return
				}
			}

			fmt.Printf("Error: No se encontró la partición con ID '%s' en el disco.\n", id)
			return
		}
	}

	fmt.Printf("Error: No hay ninguna partición montada con ID '%s'.\n", id)
}

// Función para obtener una partición montada por ID (ahora exportada)
func GetMountedPartition(id string) *MountedPartition {
	// Montajes se mantienen en memoria. Si no hay ninguno, devolvemos nil.
	// Comparación insensible a mayúsculas/minúsculas para mayor tolerancia
	idLower := strings.ToLower(id)
	for i, mounted := range mountedPartitions {
		if strings.ToLower(mounted.ID) == idLower {
			return &mountedPartitions[i]
		}
	}
	return nil
}

// GetMountedPartitionsOnly devuelve las particiones montadas en formato compatible
func GetMountedPartitionsOnly() []map[string]interface{} {
	// Montajes se mantienen en memoria. Si no hay ninguno, devolver lista vacía.
	var result []map[string]interface{}

	// Agrupar por disco
	diskMap := make(map[string][]map[string]interface{})

	for _, mp := range mountedPartitions {
		partition := map[string]interface{}{
			"id":        mp.ID,
			"name":      mp.Name,
			"type":      "Primary",
			"size":      0,
			"isMounted": true,
			"status":    "mounted",
		}

		diskMap[mp.Path] = append(diskMap[mp.Path], partition)
	}

	// Convertir a formato de discos
	for diskPath, partitions := range diskMap {
		disk := map[string]interface{}{
			"path":       diskPath,
			"size":       0,
			"unit":       "M",
			"fit":        "FF",
			"partitions": partitions,
		}
		result = append(result, disk)
	}

	return result
}

// Mount state is intentionally kept in-memory only. No persistence functions.
