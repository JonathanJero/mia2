package commands

import (
    "backend/structs"
    "encoding/binary"
    "fmt"
    "os"
    "strings"
)

// DiskInfo - Información de un disco
type DiskInfo struct {
    Path       string          `json:"path"`
    Size       int             `json:"size"`
    Unit       string          `json:"unit"`
    Fit        string          `json:"fit"`
    Partitions []PartitionInfo `json:"partitions"`
}

// PartitionInfo - Información de una partición
type PartitionInfo struct {
    Name      string `json:"name"`
    ID        string `json:"id"`
    Size      int64  `json:"size"`
    Type      string `json:"type"`
    IsMounted bool   `json:"isMounted"`
    Status    string `json:"status"`
}

// GetAllDisks - Obtener información de todos los discos creados (OPTIMIZADO)
func GetAllDisks() []DiskInfo {
    return GetAllDisksOptimized()
}

// readDiskInfoOptimized - Leer información de un disco de forma optimizada
func readDiskInfoOptimized(diskPath string) *DiskInfo {
    file, err := os.Open(diskPath)
    if err != nil {
        return nil
    }
    defer file.Close()

    // Leer solo el MBR (primeros bytes)
    var mbr structs.MBR
    if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
        return nil
    }

    // Obtener tamaño del archivo
    fileInfo, err := file.Stat()
    if err != nil {
        return nil
    }

    diskSize := fileInfo.Size()
    size := int(diskSize)
    unit := "B"

    // Convertir a unidad apropiada
    if diskSize >= 1024*1024*1024 {
        size = int(diskSize / (1024 * 1024 * 1024))
        unit = "GB"
    } else if diskSize >= 1024*1024 {
        size = int(diskSize / (1024 * 1024))
        unit = "MB"
    } else if diskSize >= 1024 {
        size = int(diskSize / 1024)
        unit = "KB"
    }

    // Obtener fit del MBR
    fit := "FF"
    fitChar := mbr.Dsk_fit
    switch fitChar {
    case 'b', 'B':
        fit = "BF"
    case 'w', 'W':
        fit = "WF"
    }

    diskInfo := &DiskInfo{
        Path:       diskPath,
        Size:       size,
        Unit:       unit,
        Fit:        fit,
        Partitions: []PartitionInfo{},
    }

    // Leer particiones primarias y extendidas
    for i := 0; i < 4; i++ {
        partition := mbr.Mbr_partitions[i]

        // Si la partición está vacía, continuar
        if partition.Part_status == 0 || partition.Part_s == 0 {
            continue
        }

        partName := strings.TrimRight(string(partition.Part_name[:]), "\x00")
        if partName == "" {
            continue
        }

        // Determinar tipo de partición
        partType := "Primaria"
        if partition.Part_type == 'e' || partition.Part_type == 'E' {
            partType = "Extendida"
        }

        // Verificar si está montada
        isMounted := false
        partID := ""
        for _, mounted := range mountedPartitions {
            if mounted.Path == diskPath && mounted.Name == partName {
                isMounted = true
                partID = mounted.ID
                break
            }
        }

        // Determinar status
        status := "No montada"
        if isMounted {
            status = "Montada"
        }

        partInfo := PartitionInfo{
            Name:      partName,
            ID:        partID,
            Size:      partition.Part_s,
            Type:      partType,
            IsMounted: isMounted,
            Status:    status,
        }

        diskInfo.Partitions = append(diskInfo.Partitions, partInfo)

        // Si es extendida, leer particiones lógicas
        if partition.Part_type == 'e' || partition.Part_type == 'E' {
            logicalPartitions := readLogicalPartitionsOptimized(file, partition.Part_start, diskPath)
            diskInfo.Partitions = append(diskInfo.Partitions, logicalPartitions...)
        }
    }

    return diskInfo
}

// readLogicalPartitionsOptimized - Leer particiones lógicas de forma optimizada
func readLogicalPartitionsOptimized(file *os.File, extendedStart int64, diskPath string) []PartitionInfo {
    var logicalPartitions []PartitionInfo

    currentEBR := extendedStart
    ebrCount := 0
    maxEBRs := 50 // Límite de seguridad aumentado

    for ebrCount < maxEBRs {
        // Buscar posición del EBR
        _, err := file.Seek(currentEBR, 0)
        if err != nil {
            fmt.Printf("Error al buscar EBR en posición %d: %v\n", currentEBR, err)
            break
        }

        var ebr structs.EBR
        if err := binary.Read(file, binary.LittleEndian, &ebr); err != nil {
            break
        }

        // Si el EBR está vacío, terminar
        if ebr.PartS == 0 {
            break
        }

        partName := strings.TrimRight(string(ebr.PartName[:]), "\x00")
        if partName != "" {
            // Verificar si está montada
            isMounted := false
            partID := ""
            for _, mounted := range mountedPartitions {
                if mounted.Path == diskPath && mounted.Name == partName {
                    isMounted = true
                    partID = mounted.ID
                    break
                }
            }

            status := "No montada"
            if isMounted {
                status = "Montada"
            }

            partInfo := PartitionInfo{
                Name:      partName,
                ID:        partID,
                Size:      ebr.PartS,
                Type:      "Lógica",
                IsMounted: isMounted,
                Status:    status,
            }

            logicalPartitions = append(logicalPartitions, partInfo)
        }

        // Si no hay siguiente EBR, terminar
        if ebr.PartNext == -1 || ebr.PartNext == 0 {
            break
        }

        currentEBR = ebr.PartNext
        ebrCount++
    }

    return logicalPartitions
}

// GetDiskByPath - Obtener información de un disco específico por su ruta
func GetDiskByPath(diskPath string) *DiskInfo {
    return readDiskInfoOptimized(diskPath)
}

// GetMountedDisks - Obtener solo los discos que tienen particiones montadas
func GetMountedDisks() []DiskInfo {
    allDisks := GetAllDisks()
    var mountedDisks []DiskInfo

    for _, disk := range allDisks {
        hasMountedPartitions := false
        for _, partition := range disk.Partitions {
            if partition.IsMounted {
                hasMountedPartitions = true
                break
            }
        }

        if hasMountedPartitions {
            mountedDisks = append(mountedDisks, disk)
        }
    }

    return mountedDisks
}

// GetDisksBasicInfo - Alias para compatibilidad
func GetDisksBasicInfo() []map[string]interface{} {
    disks := GetAllDisks()
    result := make([]map[string]interface{}, len(disks))

    for i, disk := range disks {
        partitions := make([]map[string]interface{}, len(disk.Partitions))
        for j, part := range disk.Partitions {
            partitions[j] = map[string]interface{}{
                "name":      part.Name,
                "id":        part.ID,
                "size":      part.Size,
                "type":      part.Type,
                "isMounted": part.IsMounted,
                "status":    part.Status,
            }
        }

        result[i] = map[string]interface{}{
            "path":       disk.Path,
            "size":       disk.Size,
            "unit":       disk.Unit,
            "fit":        disk.Fit,
            "partitions": partitions,
        }
    }

    return result
}