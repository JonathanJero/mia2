package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
)

func ExecuteFdisk(size int64, unit string, path string, tipo string, fit string, name string, delete string, add int64) {
    // Validar path obligatorio
    if path == "" {
        fmt.Printf("Error: El parámetro -path es obligatorio.\n")
        return
    }

    if !strings.HasSuffix(strings.ToLower(path), ".mia") {
        path += ".mia"
    }

    // Verificar que el archivo existe
    if _, err := os.Stat(path); os.IsNotExist(err) {
        fmt.Printf("Error: El archivo '%s' no existe.\n", path)
        return
    }

    // CASO 1: DELETE - Eliminar partición
    if delete != "" {
        if name == "" {
            fmt.Printf("Error: El parámetro -name es obligatorio para eliminar una partición.\n")
            return
        }
        executeDelete(path, name, delete)
        return
    }

    // CASO 2: ADD - Agregar/quitar espacio a partición existente
    if add != 0 {
        if name == "" {
            fmt.Printf("Error: El parámetro -name es obligatorio para modificar el tamaño de una partición.\n")
            return
        }
        if unit == "" {
            unit = "K" // Por defecto Kilobytes
        }
        executeAdd(path, name, add, unit)
        return
    }

    // CASO 3: CREATE - Crear nueva partición
    // Validar parámetros obligatorios SOLO para CREATE
    if size <= 0 {
        fmt.Printf("Error: El parámetro -size es obligatorio y debe ser positivo para crear una partición.\n")
        return
    }

    if name == "" {
        fmt.Printf("Error: El parámetro -name es obligatorio.\n")
        return
    }

    // Manejar parámetros opcionales con valores por defecto
    if tipo == "" {
        tipo = "P" // Por defecto primaria
    }

    if fit == "" {
        fit = "WF" // Por defecto Worst Fit
    }

    if unit == "" {
        unit = "K" // Por defecto Kilobytes
    }

    // Validar unidadesb
    unit = strings.ToUpper(unit)
    if unit != "B" && unit != "K" && unit != "M" {
        fmt.Printf("Error: Unidad '%s' no válida. Use 'B', 'K' o 'M'.\n", unit)
        return
    }

    // Validar tipos
    tipo = strings.ToUpper(tipo)
    if tipo != "P" && tipo != "E" && tipo != "L" {
        fmt.Printf("Error: Tipo de partición '%s' no válido. Use 'P', 'E' o 'L'.\n", tipo)
        return
    }

    // Validar fit
    fit = strings.ToUpper(fit)
    if fit != "BF" && fit != "FF" && fit != "WF" {
        fmt.Printf("Error: Ajuste '%s' no válido. Use 'BF', 'FF' o 'WF'.\n", fit)
        return
    }

    file, err := os.OpenFile(path, os.O_RDWR, 0644)
    if err != nil {
        fmt.Printf("Error al abrir el archivo: %v\n", err)
        return
    }
    defer file.Close()

    var mbr structs.MBR
    if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
        fmt.Printf("Error al leer el MBR: %v\n", err)
        return
    }

    // Convertir tamaño según la unidad
    sizeInBytes := convertSize(size, unit)

    // Validar nombre duplicado
    if err := validatePartitionName(name, &mbr); err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    // Validar restricciones de particiones
    if err := validatePartitionConstraints(tipo, &mbr); err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    // Validar espacio disponible
    if err := validateAvailableSpace(sizeInBytes, &mbr, tipo); err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    // Buscar un slot libre en las particiones (solo para P y E)
    var partitionIndex int = -1
    var startPosition int64

    switch tipo {
    case "P", "E":
        partitionIndex = findFreePartitionSlot(&mbr)
        if partitionIndex == -1 {
            fmt.Printf("Error: No hay slots disponibles para particiones primarias/extendidas.\n")
            return
        }
        startPosition = calculateStartPosition(&mbr, fit, sizeInBytes)
    case "L":
        // Para particiones lógicas, usar la partición extendida
        _, err = handleLogicalPartition(&mbr, sizeInBytes, fit, name, file)
        if err != nil {
            fmt.Printf("Error: %v\n", err)
            return
        }
        fmt.Printf("Partición lógica '%s' creada exitosamente en '%s'.\n", name, path)
        return
    }

    // Crear la nueva partición
    newPartition := structs.NewPartition(
        '1',           // status: activa
        tipo[0],       // tipo: P, E, o L
        fit[0],        // fit: primer carácter de fit
        startPosition, // posición de inicio
        sizeInBytes,   // tamaño en bytes
        [16]byte{},    // nombre (se copia después)
    )

    // Copiar el nombre de la partición
    copy(newPartition.Part_name[:], []byte(name))

    // Asignar la partición al MBR
    mbr.Mbr_partitions[partitionIndex] = newPartition

    // Escribir el MBR actualizado al inicio del archivo
    file.Seek(0, 0)
    if err := binary.Write(file, binary.LittleEndian, &mbr); err != nil {
        fmt.Printf("Error al escribir el MBR: %v\n", err)
        return
    }

    var tipoNombre string
    switch tipo {
    case "P":
        tipoNombre = "Primaria"
    case "E":
        tipoNombre = "Extendida"
    case "L":
        tipoNombre = "Lógica"
    }

    fmt.Printf("Partición '%s' de tipo '%s' creada exitosamente en '%s'.\n", name, tipoNombre, path)
    fmt.Printf("Tamaño: %d bytes, Ajuste: %s, Posición: %d\n", sizeInBytes, fit, startPosition)

    if err := file.Sync(); err != nil {
        fmt.Printf("Error al sincronizar el archivo: %v\n", err)
        return
    }
}

// executeDelete - Eliminar una partición
func executeDelete(path string, name string, deleteType string) {
    // Validar tipo de eliminación
    deleteType = strings.ToLower(deleteType)
    if deleteType != "fast" && deleteType != "full" {
        fmt.Printf("Error: Tipo de eliminación '%s' no válido. Use 'fast' o 'full'.\n", deleteType)
        return
    }

    // Abrir archivo
    file, err := os.OpenFile(path, os.O_RDWR, 0644)
    if err != nil {
        fmt.Printf("Error al abrir el archivo: %v\n", err)
        return
    }
    defer file.Close()

    // Leer MBR
    var mbr structs.MBR
    if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
        fmt.Printf("Error al leer el MBR: %v\n", err)
        return
    }

    // PRIMERO: Buscar en particiones primarias/extendidas
    partitionIndex := -1
    var partition *structs.Partition

    for i := 0; i < 4; i++ {
        if mbr.Mbr_partitions[i].Part_s > 0 {
            partitionName := strings.TrimSpace(string(mbr.Mbr_partitions[i].Part_name[:]))
            partitionName = strings.TrimRight(partitionName, "\x00")
            
            if partitionName == name {
                partitionIndex = i
                partition = &mbr.Mbr_partitions[i]
                break
            }
        }
    }

    // Si no se encontró, buscar en particiones lógicas
    if partitionIndex == -1 {
        fmt.Printf("🔍 Buscando en particiones lógicas...\n")
        
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
            // Buscar en la cadena de EBRs
            if err := deleteLogicalPartitionByName(file, extendedPartition, name, deleteType); err != nil {
                fmt.Printf("Error: %v\n", err)
            }
            return
        }

        fmt.Printf("Error: No se encontró la partición '%s'.\n", name)
        return
    }

    // Si es extendida, eliminar particiones lógicas
    if partition.Part_type == 'E' || partition.Part_type == 'e' {
        fmt.Printf("🗑️  Eliminando particiones lógicas dentro de la partición extendida...\n")
        if err := deleteLogicalPartitions(file, partition, deleteType); err != nil {
            fmt.Printf("⚠️  Advertencia al eliminar particiones lógicas: %v\n", err)
        }
    }

    // Tipo de eliminación
    if deleteType == "full" {
        fmt.Printf("🔄 Eliminación completa: rellenando con \\0...\n")
        fillWithZeros(file, partition.Part_start, partition.Part_s)
    }

    // Marcar partición como vacía
    mbr.Mbr_partitions[partitionIndex].Part_status = '0'
    mbr.Mbr_partitions[partitionIndex].Part_type = '0'
    mbr.Mbr_partitions[partitionIndex].Part_fit = '0'
    mbr.Mbr_partitions[partitionIndex].Part_start = 0
    mbr.Mbr_partitions[partitionIndex].Part_s = 0
    for i := range mbr.Mbr_partitions[partitionIndex].Part_name {
        mbr.Mbr_partitions[partitionIndex].Part_name[i] = 0
    }

    // Reorganizar particiones
    fmt.Printf("🔧 Reorganizando particiones...\n")
    defragmentPartitions(&mbr, file)

    // Escribir MBR actualizado
    file.Seek(0, 0)
    if err := binary.Write(file, binary.LittleEndian, &mbr); err != nil {
        fmt.Printf("Error al escribir el MBR: %v\n", err)
        return
    }

    fmt.Printf("✅ Partición '%s' eliminada exitosamente (%s).\n", name, deleteType)
}

// Eliminar partición lógica por nombre
func deleteLogicalPartitionByName(file *os.File, extendedPartition *structs.Partition, name string, deleteType string) error {
    currentEBRPos := extendedPartition.Part_start
    var prevEBRPos int64 = -1

    for currentEBRPos != -1 {
        file.Seek(currentEBRPos, 0)
        var ebr structs.EBR
        if err := binary.Read(file, binary.LittleEndian, &ebr); err != nil {
            break
        }

        ebrName := strings.TrimSpace(string(ebr.PartName[:]))
        ebrName = strings.TrimRight(ebrName, "\x00")

        // Si encontramos la partición
        if ebrName == name {
            fmt.Printf("✅ Partición lógica '%s' encontrada\n", name)

            // Eliminar contenido si es FULL
            if deleteType == "full" {
                fmt.Printf("🔄 Eliminación completa: rellenando con \\0...\n")
                fillWithZeros(file, ebr.PartStart, ebr.PartS)
            }

            // Actualizar la cadena de EBRs
            if prevEBRPos != -1 {
                // Si hay un EBR anterior, apuntar al siguiente
                file.Seek(prevEBRPos, 0)
                var prevEBR structs.EBR
                binary.Read(file, binary.LittleEndian, &prevEBR)
                prevEBR.PartNext = ebr.PartNext
                file.Seek(prevEBRPos, 0)
                binary.Write(file, binary.LittleEndian, &prevEBR)
            }

            // Limpiar el EBR actual
            file.Seek(currentEBRPos, 0)
            emptyEBR := structs.EBR{}
            emptyEBR.PartNext = -1
            binary.Write(file, binary.LittleEndian, &emptyEBR)

            fmt.Printf("✅ Partición lógica '%s' eliminada exitosamente (%s).\n", name, deleteType)
            return nil
        }

        prevEBRPos = currentEBRPos
        currentEBRPos = ebr.PartNext
    }

    return fmt.Errorf("no se encontró la partición lógica '%s'", name)
}

/// defragmentPartitions - Reorganizar particiones para consolidar espacio libre
func defragmentPartitions(mbr *structs.MBR, file *os.File) {
    fmt.Printf("🔧 Desfragmentando particiones...\n")

    // Paso 1: Ordenar particiones por posición de inicio
    type PartitionInfo struct {
        Index int
        Start int64
        Size  int64
        Part  structs.Partition
    }

    var activePartitions []PartitionInfo
    for i := 0; i < 4; i++ {
        if mbr.Mbr_partitions[i].Part_s > 0 && mbr.Mbr_partitions[i].Part_status != '0' {
            activePartitions = append(activePartitions, PartitionInfo{
                Index: i,
                Start: mbr.Mbr_partitions[i].Part_start,
                Size:  mbr.Mbr_partitions[i].Part_s,
                Part:  mbr.Mbr_partitions[i],
            })
        }
    }

    // Si no hay particiones activas o solo hay una, no hay nada que hacer
    if len(activePartitions) <= 1 {
        fmt.Printf("   ℹ️  No es necesario desfragmentar (solo %d partición activa)\n", len(activePartitions))
        return
    }

    // Ordenar por posición de inicio
    for i := 0; i < len(activePartitions)-1; i++ {
        for j := i + 1; j < len(activePartitions); j++ {
            if activePartitions[j].Start < activePartitions[i].Start {
                activePartitions[i], activePartitions[j] = activePartitions[j], activePartitions[i]
            }
        }
    }

    // Paso 2: Calcular nuevas posiciones y mover particiones si es necesario
    // ✅ CORRECCIÓN: Usar 512 en lugar de binary.Size()
    const mbrSize = int64(512) // Tamaño fijo del MBR en disco
    currentPos := mbrSize

    moved := false
    for i := 0; i < len(activePartitions); i++ {
        oldStart := activePartitions[i].Start
        newStart := currentPos

        // Si la partición necesita moverse
        if oldStart != newStart {
            partName := strings.TrimRight(string(activePartitions[i].Part.Part_name[:]), "\x00")
            fmt.Printf("   📦 Moviendo partición '%s' de posición %d a %d\n",
                partName,
                oldStart,
                newStart)

            // Leer datos de la partición
            partitionData := make([]byte, activePartitions[i].Size)
            file.Seek(oldStart, 0)
            if _, err := file.Read(partitionData); err != nil {
                fmt.Printf("   ⚠️  Error al leer partición: %v\n", err)
                continue
            }

            // Escribir en la nueva posición
            file.Seek(newStart, 0)
            if _, err := file.Write(partitionData); err != nil {
                fmt.Printf("   ⚠️  Error al escribir partición: %v\n", err)
                continue
            }

            // Limpiar la posición antigua (solo si no se solapa)
            if oldStart > newStart+activePartitions[i].Size {
                fillWithZeros(file, oldStart, activePartitions[i].Size)
            }

            // Actualizar la posición en el MBR
            mbr.Mbr_partitions[activePartitions[i].Index].Part_start = newStart
            activePartitions[i].Start = newStart
            moved = true
        }

        // Actualizar posición para la siguiente partición
        currentPos = newStart + activePartitions[i].Size
    }

    // Paso 3: Limpiar slots vacíos y reorganizar
    // Crear nueva lista ordenada de particiones
    var newPartitions [4]structs.Partition
    newIndex := 0

    for i := 0; i < len(activePartitions); i++ {
        newPartitions[newIndex] = mbr.Mbr_partitions[activePartitions[i].Index]
        newPartitions[newIndex].Part_start = activePartitions[i].Start
        newIndex++
    }

    // Llenar el resto con particiones vacías
    for i := newIndex; i < 4; i++ {
        newPartitions[i] = structs.Partition{}
        newPartitions[i].Part_status = '0'
        newPartitions[i].Part_start = 0
        newPartitions[i].Part_s = 0
    }

    // Actualizar el MBR
    mbr.Mbr_partitions = newPartitions

    // Escribir el MBR actualizado
    file.Seek(0, 0)
    if err := binary.Write(file, binary.LittleEndian, mbr); err != nil {
        fmt.Printf("   ⚠️  Error al escribir MBR: %v\n", err)
        return
    }

    if moved {
        fmt.Printf("   ✅ Particiones reorganizadas exitosamente\n")
    } else {
        fmt.Printf("   ℹ️  Las particiones ya están en posiciones óptimas\n")
    }
}

// deleteLogicalPartitions - Eliminar todas las particiones lógicas dentro de una extendida
func deleteLogicalPartitions(file *os.File, extendedPartition *structs.Partition, deleteType string) error {
	// Leer el primer EBR
	currentEBRPos := extendedPartition.Part_start

	for currentEBRPos != -1 {
		file.Seek(currentEBRPos, 0)
		var ebr structs.EBR
		if err := binary.Read(file, binary.LittleEndian, &ebr); err != nil {
			break
		}

		// Si hay una partición lógica válida
		if ebr.PartS > 0 {
			logicalName := strings.TrimSpace(string(ebr.PartName[:]))
			fmt.Printf("   🗑️  Eliminando partición lógica '%s'...\n", logicalName)

			if deleteType == "full" {
				fillWithZeros(file, ebr.PartStart, ebr.PartS)
			}
		}

		// Ir al siguiente EBR
		currentEBRPos = ebr.PartNext
	}

	return nil
}

// fillWithZeros - Rellenar un rango del archivo con \0
func fillWithZeros(file *os.File, start int64, size int64) {
	file.Seek(start, 0)

	// Escribir en bloques de 4KB para mejor rendimiento
	blockSize := int64(4096)
	zeros := make([]byte, blockSize)

	remaining := size
	for remaining > 0 {
		writeSize := blockSize
		if remaining < blockSize {
			writeSize = remaining
		}
		file.Write(zeros[:writeSize])
		remaining -= writeSize
	}
}

// executeAdd - Agregar o quitar espacio de una partición
func executeAdd(path string, name string, add int64, unit string) {
    // Validar unidad
    unit = strings.ToUpper(unit)
    if unit != "B" && unit != "K" && unit != "M" {
        fmt.Printf("Error: Unidad '%s' no válida. Use 'B', 'K' o 'M'.\n", unit)
        return
    }

    // Convertir tamaño según la unidad
    addBytes := convertSize(add, unit)

    // Abrir archivo
    file, err := os.OpenFile(path, os.O_RDWR, 0644)
    if err != nil {
        fmt.Printf("Error al abrir el archivo: %v\n", err)
        return
    }
    defer file.Close()

    // Leer MBR
    var mbr structs.MBR
    if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
        fmt.Printf("Error al leer el MBR: %v\n", err)
        return
    }

    // Buscar la partición
    partitionIndex := -1
    var partition *structs.Partition

    for i := 0; i < 4; i++ {
        if mbr.Mbr_partitions[i].Part_s > 0 {
            partitionName := strings.TrimSpace(string(mbr.Mbr_partitions[i].Part_name[:]))
            partitionName = strings.TrimRight(partitionName, "\x00")
            
            if partitionName == name {
                partitionIndex = i
                partition = &mbr.Mbr_partitions[i]
                break
            }
        }
    }

    if partitionIndex == -1 {
        fmt.Printf("Error: No se encontró la partición '%s'.\n", name)
        return
    }

    // Calcular nuevo tamaño
    newSize := partition.Part_s + addBytes

    // Validar que el nuevo tamaño no sea negativo
    if newSize < 0 {
        fmt.Printf("Error: El nuevo tamaño de la partición sería negativo o cero. Tamaño actual: %d bytes, Cambio: %d bytes.\n",
            partition.Part_s, addBytes)
        return
    }

    // Guardar tamaño anterior
    oldSize := partition.Part_s
    oldStart := partition.Part_start

    // Si se está REDUCIENDO el tamaño (add negativo)
    if addBytes < 0 {
        // Limpiar desde el nuevo final hasta el final anterior
        fillWithZeros(file, oldStart+newSize, -addBytes)

        // Actualizar tamaño de la partición
        mbr.Mbr_partitions[partitionIndex].Part_s = newSize

        // ✅ Reorganizar particiones para consolidar espacio libre
        fmt.Printf("🔧 Reorganizando particiones para consolidar espacio libre...\n")
        defragmentPartitions(&mbr, file)

        // Releer el MBR después de desfragmentar
        file.Seek(0, 0)
        if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
            fmt.Printf("Error al releer el MBR: %v\n", err)
            return
        }

        // Buscar de nuevo la partición después de desfragmentar
        for i := 0; i < 4; i++ {
            if mbr.Mbr_partitions[i].Part_s > 0 {
                partName := strings.TrimSpace(string(mbr.Mbr_partitions[i].Part_name[:]))
                partName = strings.TrimRight(partName, "\x00")
                if partName == name {
                    partitionIndex = i
                    break
                }
            }
        }
    } else {
        // ✅ Si se está AGREGANDO espacio (add positivo)
        
        // Primero intentar espacio inmediatamente después
        availableAfter := calculateAvailableSpaceAfter(partition, &mbr)
        
        // Si no hay espacio después, verificar si hay espacio al final del disco
        if addBytes > availableAfter {
            // Calcular espacio total disponible en el disco
            totalAvailable := calculateTotalAvailableSpace(&mbr)
            
            if addBytes > totalAvailable {
                fmt.Printf("Error: No hay suficiente espacio disponible en el disco.\n")
                fmt.Printf("   Espacio disponible total: %d bytes\n", totalAvailable)
                fmt.Printf("   Espacio solicitado: %d bytes\n", addBytes)
                return
            }
            
            // Si hay espacio en el disco pero no después de la partición,
            // necesitamos reorganizar
            fmt.Printf("🔧 Reorganizando particiones para crear espacio contiguo...\n")
            
            // Actualizar tamaño temporalmente
            mbr.Mbr_partitions[partitionIndex].Part_s = newSize
            
            // Desfragmentar (esto moverá las particiones para crear espacio)
            defragmentPartitions(&mbr, file)
            
            // Releer el MBR
            file.Seek(0, 0)
            if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
                fmt.Printf("Error al releer el MBR: %v\n", err)
                return
            }
            
            // Buscar de nuevo la partición
            for i := 0; i < 4; i++ {
                if mbr.Mbr_partitions[i].Part_s > 0 {
                    partName := strings.TrimSpace(string(mbr.Mbr_partitions[i].Part_name[:]))
                    partName = strings.TrimRight(partName, "\x00")
                    if partName == name {
                        partitionIndex = i
                        break
                    }
                }
            }
        } else {
            // Si hay espacio suficiente después, simplemente actualizar el tamaño
            mbr.Mbr_partitions[partitionIndex].Part_s = newSize
            
            // Escribir el MBR
            file.Seek(0, 0)
            if err := binary.Write(file, binary.LittleEndian, &mbr); err != nil {
                fmt.Printf("Error al escribir el MBR: %v\n", err)
                return
            }
        }
    }

    // Mostrar resultado
    if addBytes > 0 {
        fmt.Printf("✅ Se agregaron %d bytes a la partición '%s'.\n", addBytes, name)
    } else {
        fmt.Printf("✅ Se quitaron %d bytes de la partición '%s'.\n", -addBytes, name)
    }
    fmt.Printf("   Tamaño anterior: %d bytes\n", oldSize)
    fmt.Printf("   Tamaño nuevo: %d bytes\n", mbr.Mbr_partitions[partitionIndex].Part_s)

    // ✅ Calcular y mostrar espacio disponible después
    availableAfter := calculateAvailableSpaceAfter(&mbr.Mbr_partitions[partitionIndex], &mbr)
    fmt.Printf("   💾 Espacio disponible después: %d bytes (%d MB)\n", availableAfter, availableAfter/(1024*1024))
}

// ✅ NUEVA FUNCIÓN: Calcular espacio total disponible en el disco
func calculateTotalAvailableSpace(mbr *structs.MBR) int64 {
    usedSpace := int64(512) // MBR
    
    for i := 0; i < 4; i++ {
        if mbr.Mbr_partitions[i].Part_s > 0 && mbr.Mbr_partitions[i].Part_status != '0' {
            usedSpace += mbr.Mbr_partitions[i].Part_s
        }
    }
    
    return mbr.Mbr_tamano - usedSpace
}

// calculateAvailableSpaceAfter - Calcular espacio libre después de una partición
func calculateAvailableSpaceAfter(partition *structs.Partition, mbr *structs.MBR) int64 {
    endOfPartition := partition.Part_start + partition.Part_s

    // Buscar la siguiente partición más cercana
    nextStart := mbr.Mbr_tamano // Por defecto, hasta el final del disco

    for _, p := range mbr.Mbr_partitions {
        // Verificar que tenga tamaño (está activa)
        if p.Part_s > 0 && p.Part_start > partition.Part_start {
            if p.Part_start < nextStart {
                nextStart = p.Part_start
            }
        }
    }

    return nextStart - endOfPartition
}

// Validar nombre duplicado
func validatePartitionName(name string, mbr *structs.MBR) error {
	for _, partition := range mbr.Mbr_partitions {
		if partition.Part_status != '0' {
			partitionName := strings.TrimSpace(string(partition.Part_name[:]))
			if partitionName == name {
				return fmt.Errorf("ya existe una partición con el nombre '%s'", name)
			}
		}
	}
	return nil
}

// Validar restricciones de particiones
func validatePartitionConstraints(tipo string, mbr *structs.MBR) error {
	primaryCount := 0
	extendedCount := 0

	for _, partition := range mbr.Mbr_partitions {
		if partition.Part_status != '0' {
			switch partition.Part_type {
			case 'P':
				primaryCount++
			case 'E':
				extendedCount++
			}
		}
	}

	// Restricción: máximo 4 particiones primarias + extendidas
	if tipo == "P" || tipo == "E" {
		if primaryCount+extendedCount >= 4 {
			return fmt.Errorf("no se pueden crear más particiones. Máximo 4 particiones primarias/extendidas")
		}
	}

	// Restricción: solo una partición extendida por disco
	if tipo == "E" && extendedCount >= 1 {
		return fmt.Errorf("ya existe una partición extendida en el disco")
	}

	// Restricción: no se puede crear partición lógica sin extendida
	if tipo == "L" && extendedCount == 0 {
		return fmt.Errorf("no se puede crear una partición lógica sin una partición extendida")
	}

	return nil
}

// Validar espacio disponible
func validateAvailableSpace(sizeInBytes int64, mbr *structs.MBR, tipo string) error {
	if tipo == "L" {
		return validateLogicalPartitionSpace(sizeInBytes, mbr)
	}

	// Para particiones primarias/extendidas
	usedSpace := int64(512) // MBR
	for _, partition := range mbr.Mbr_partitions {
		if partition.Part_status != '0' && partition.Part_s > 0 {
			usedSpace += partition.Part_s
		}
	}

	availableSpace := mbr.Mbr_tamano - usedSpace

	if sizeInBytes > availableSpace {
		return fmt.Errorf("no hay espacio suficiente en el disco. Disponible: %d bytes, Requerido: %d bytes",
			availableSpace, sizeInBytes)
	}

	return nil
}

// Validar espacio en partición lógica
func validateLogicalPartitionSpace(sizeInBytes int64, mbr *structs.MBR) error {
    // Encontrar la partición extendida
    var extendedPartition *structs.Partition
    for i := 0; i < 4; i++ {
        if mbr.Mbr_partitions[i].Part_s > 0 && mbr.Mbr_partitions[i].Part_type == 'E' {
            extendedPartition = &mbr.Mbr_partitions[i]
            break
        }
    }

    if extendedPartition == nil {
        return fmt.Errorf("no se encontró partición extendida")
    }

    // Abrir el archivo del disco
    // Nota: Esta función necesita acceso al archivo, se debe pasar como parámetro
    // Por ahora, retornar nil como placeholder
    // La validación real se hará en handleLogicalPartition

    // Reservar espacio para el primer EBR (1024 bytes)
    const ebrSize = int64(1024)
    
    // El espacio mínimo necesario es: tamaño solicitado + espacio para EBR
    requiredSpace := sizeInBytes + ebrSize

    if requiredSpace > extendedPartition.Part_s {
        return fmt.Errorf("no hay espacio suficiente en la partición extendida. Disponible: %d bytes, Requerido: %d bytes",
            extendedPartition.Part_s, requiredSpace)
    }

    return nil
}

// Calcular espacio usado por particiones lógicas
func calculateLogicalPartitionsUsedSpace(file *os.File, extendedPartition *structs.Partition) (int64, error) {
    usedSpace := int64(0)
    const ebrSize = int64(1024) // Tamaño de cada EBR

    // Leer la cadena de EBRs
    currentEBRPos := extendedPartition.Part_start

    for currentEBRPos != -1 {
        // Leer EBR
        file.Seek(currentEBRPos, 0)
        var ebr structs.EBR
        if err := binary.Read(file, binary.LittleEndian, &ebr); err != nil {
            // Si hay error leyendo, asumir que no hay EBRs (partición vacía)
            break
        }

        // Si el EBR tiene una partición válida
        if ebr.PartS > 0 {
            // Sumar: tamaño del EBR + tamaño de la partición
            usedSpace += ebrSize + ebr.PartS
            
            // Ir al siguiente EBR
            currentEBRPos = ebr.PartNext
        } else {
            // Si el EBR no tiene partición (PartS == 0), la partición extendida está vacía
            // No contar ningún espacio usado
            break
        }
    }

    return usedSpace, nil
}

// Validar nombre duplicado en particiones lógicas
func validateLogicalPartitionName(file *os.File, extendedPartition *structs.Partition, name string) error {
    currentEBRPos := extendedPartition.Part_start

    for currentEBRPos != -1 {
        file.Seek(currentEBRPos, 0)
        var ebr structs.EBR
        if err := binary.Read(file, binary.LittleEndian, &ebr); err != nil {
            break
        }

        if ebr.PartS > 0 {
            ebrName := strings.TrimSpace(string(ebr.PartName[:]))
            ebrName = strings.TrimRight(ebrName, "\x00")

            if ebrName == name {
                return fmt.Errorf("ya existe una partición lógica con el nombre '%s'", name)
            }
        }

        currentEBRPos = ebr.PartNext
    }

    return nil
}
// Buscar slot libre
func findFreePartitionSlot(mbr *structs.MBR) int {
	for i := 0; i < 4; i++ {
		if mbr.Mbr_partitions[i].Part_status == '0' {
			return i
		}
	}
	return -1
}

// Función para manejar diferentes ajustes
func calculateStartPosition(mbr *structs.MBR, fit string, sizeNeeded int64) int64 {
	switch fit {
	case "FF": // First Fit
		return calculateFirstFit(mbr, sizeNeeded)
	case "BF": // Best Fit
		return calculateBestFit(mbr, sizeNeeded)
	case "WF": // Worst Fit
		return calculateWorstFit(mbr, sizeNeeded)
	default:
		return calculateWorstFit(mbr, sizeNeeded)
	}
}

// ← IMPLEMENTACIÓN COMPLETA: Best Fit - encuentra el espacio libre más pequeño que sea suficiente
func calculateBestFit(mbr *structs.MBR, sizeNeeded int64) int64 {
	// Obtener todos los espacios libres
	freeSpaces := getFreeSpaces(mbr)

	if len(freeSpaces) == 0 {
		return int64(512) // Después del MBR si no hay particiones
	}

	// Encontrar el espacio libre más pequeño que sea suficiente
	bestStart := int64(-1)
	bestSize := int64(math.MaxInt64)

	for _, space := range freeSpaces {
		if space.Size >= sizeNeeded && space.Size < bestSize {
			bestSize = space.Size
			bestStart = space.Start
		}
	}

	// Si no se encontró espacio suficiente, usar el último espacio disponible
	if bestStart == -1 && len(freeSpaces) > 0 {
		return freeSpaces[len(freeSpaces)-1].Start
	}

	if bestStart == -1 {
		return int64(512)
	}

	return bestStart
}

// ← IMPLEMENTACIÓN COMPLETA: Worst Fit - encuentra el espacio libre más grande
func calculateWorstFit(mbr *structs.MBR, sizeNeeded int64) int64 {
	// Obtener todos los espacios libres
	freeSpaces := getFreeSpaces(mbr)

	if len(freeSpaces) == 0 {
		return int64(512) // Después del MBR si no hay particiones
	}

	// Encontrar el espacio libre más grande
	worstStart := int64(-1)
	worstSize := int64(0)

	for _, space := range freeSpaces {
		if space.Size >= sizeNeeded && space.Size > worstSize {
			worstSize = space.Size
			worstStart = space.Start
		}
	}

	// Si no se encontró espacio suficiente, usar el último espacio disponible
	if worstStart == -1 && len(freeSpaces) > 0 {
		return freeSpaces[len(freeSpaces)-1].Start
	}

	if worstStart == -1 {
		return int64(512)
	}

	return worstStart
}

// ESTRUCTURA: Para representar espacios libres
type FreeSpace struct {
	Start int64
	Size  int64
}

// Obtener todos los espacios libres en el disco
func getFreeSpaces(mbr *structs.MBR) []FreeSpace {
	var freeSpaces []FreeSpace
	var usedRanges []FreeSpace

	// Agregar el rango del MBR como usado
	usedRanges = append(usedRanges, FreeSpace{Start: 0, Size: 512})

	// Recopilar todas las particiones existentes
	for _, partition := range mbr.Mbr_partitions {
		if partition.Part_status != '0' {
			usedRanges = append(usedRanges, FreeSpace{
				Start: partition.Part_start,
				Size:  partition.Part_s,
			})
		}
	}

	// Ordenar los rangos usados por posición de inicio
	for i := 0; i < len(usedRanges)-1; i++ {
		for j := i + 1; j < len(usedRanges); j++ {
			if usedRanges[i].Start > usedRanges[j].Start {
				usedRanges[i], usedRanges[j] = usedRanges[j], usedRanges[i]
			}
		}
	}

	// Encontrar espacios libres entre particiones
	currentPos := int64(0)

	for _, used := range usedRanges {
		// Si hay espacio libre antes de esta partición
		if used.Start > currentPos {
			freeSpaces = append(freeSpaces, FreeSpace{
				Start: currentPos,
				Size:  used.Start - currentPos,
			})
		}

		// Actualizar la posición actual al final de esta partición
		endPos := used.Start + used.Size
		if endPos > currentPos {
			currentPos = endPos
		}
	}

	// Agregar espacio libre al final del disco si existe
	if currentPos < mbr.Mbr_tamano {
		freeSpaces = append(freeSpaces, FreeSpace{
			Start: currentPos,
			Size:  mbr.Mbr_tamano - currentPos,
		})
	}

	return freeSpaces
}

// ← MEJORAR: First Fit - encuentra el primer espacio libre suficiente
func calculateFirstFit(mbr *structs.MBR, sizeNeeded int64) int64 {
	// Obtener todos los espacios libres
	freeSpaces := getFreeSpaces(mbr)

	if len(freeSpaces) == 0 {
		return int64(512) // Después del MBR si no hay particiones
	}

	// Encontrar el primer espacio libre que sea suficiente
	for _, space := range freeSpaces {
		if space.Size >= sizeNeeded {
			return space.Start
		}
	}

	// Si no se encontró espacio suficiente, usar el último espacio disponible
	return freeSpaces[len(freeSpaces)-1].Start
}

// Manejar particiones lógicas con validación completa
func handleLogicalPartition(mbr *structs.MBR, sizeInBytes int64, fit string, name string, file *os.File) (int64, error) {
    // Encontrar la partición extendida
    var extendedPartition *structs.Partition
    for i := 0; i < 4; i++ {
        if mbr.Mbr_partitions[i].Part_s > 0 && mbr.Mbr_partitions[i].Part_type == 'E' {
            extendedPartition = &mbr.Mbr_partitions[i]
            break
        }
    }

    if extendedPartition == nil {
        return 0, fmt.Errorf("no se encontró partición extendida")
    }

    // Validar nombre duplicado en particiones lógicas
    if err := validateLogicalPartitionName(file, extendedPartition, name); err != nil {
        return 0, err
    }

    const ebrSize = int64(1024)

    // Calcular espacio usado por particiones lógicas existentes
    usedSpace, err := calculateLogicalPartitionsUsedSpace(file, extendedPartition)
    if err != nil {
        return 0, fmt.Errorf("error calculando espacio usado: %v", err)
    }

    // Calcular espacio disponible
    availableSpace := extendedPartition.Part_s - usedSpace

    // Validar que hay espacio suficiente (tamaño solicitado + EBR)
    requiredSpace := sizeInBytes + ebrSize
    if requiredSpace > availableSpace {
        return 0, fmt.Errorf("no hay espacio suficiente en la partición extendida. Disponible: %d bytes, Requerido: %d bytes",
            availableSpace, requiredSpace)
    }

    // Buscar la última partición lógica para encadenar
    var lastEBRPos int64 = -1
    var isFirstLogical bool = true
    currentEBRPos := extendedPartition.Part_start

    // Intentar leer el primer EBR
    file.Seek(currentEBRPos, 0)
    var firstEBR structs.EBR
    if err := binary.Read(file, binary.LittleEndian, &firstEBR); err == nil {
        // Si se pudo leer y tiene partición válida, hay particiones lógicas
        if firstEBR.PartS > 0 {
            isFirstLogical = false
            
            // Buscar la última partición lógica
            currentEBRPos = extendedPartition.Part_start
            for {
                file.Seek(currentEBRPos, 0)
                var ebr structs.EBR
                if err := binary.Read(file, binary.LittleEndian, &ebr); err != nil {
                    break
                }

                // Si no tiene partición válida, salir
                if ebr.PartS == 0 {
                    break
                }

                // Si no hay siguiente EBR, esta es la última
                if ebr.PartNext == -1 {
                    lastEBRPos = currentEBRPos
                    break
                }

                currentEBRPos = ebr.PartNext
            }
        }
    }

    // Calcular posición para el nuevo EBR
    var newEBRPos int64
    if isFirstLogical {
        // Primera partición lógica - empieza al inicio de la partición extendida
        newEBRPos = extendedPartition.Part_start
    } else {
        // Leer el último EBR para calcular la posición del nuevo
        file.Seek(lastEBRPos, 0)
        var lastEBR structs.EBR
        binary.Read(file, binary.LittleEndian, &lastEBR)

        // Nueva posición: después del último EBR y su partición
        newEBRPos = lastEBR.PartStart + lastEBR.PartS
    }

    // Validar que la nueva posición está dentro de la partición extendida
    if newEBRPos+requiredSpace > extendedPartition.Part_start+extendedPartition.Part_s {
        return 0, fmt.Errorf("no hay espacio contiguo suficiente para la partición lógica")
    }

    // Crear nuevo EBR
    newEBR := structs.EBR{
        PartMount: 0,
        PartFit:   fit[0],
        PartStart: newEBRPos + ebrSize, // Partición empieza después del EBR
        PartS:     sizeInBytes,
        PartNext:  -1, // Última partición lógica
    }
    copy(newEBR.PartName[:], []byte(name))

    // Si no es la primera partición lógica, actualizar el EBR anterior
    if !isFirstLogical && lastEBRPos != -1 {
        file.Seek(lastEBRPos, 0)
        var lastEBR structs.EBR
        binary.Read(file, binary.LittleEndian, &lastEBR)

        // Actualizar el puntero Next
        lastEBR.PartNext = newEBRPos

        // Escribir el EBR actualizado
        file.Seek(lastEBRPos, 0)
        if err := binary.Write(file, binary.LittleEndian, &lastEBR); err != nil {
            return 0, fmt.Errorf("error actualizando EBR anterior: %v", err)
        }
    }

    // Escribir el nuevo EBR
    file.Seek(newEBRPos, 0)
    if err := binary.Write(file, binary.LittleEndian, &newEBR); err != nil {
        return 0, fmt.Errorf("error escribiendo EBR: %v", err)
    }

    return newEBRPos, nil
}

func convertSize(size int64, unit string) int64 {
	switch strings.ToUpper(unit) {
	case "K":
		return size * 1024
	case "M":
		return size * 1024 * 1024
	case "B":
		return size
	default:
		return size * 1024 // Por defecto Kilobytes
	}
}
