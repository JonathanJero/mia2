package commands

import (
    "encoding/json"
    "os"
    "path/filepath"
    "sync"
)

// REGISTRO DE DISCOS PERSISTENTE
type DiskRegistry struct {
    Disks []string `json:"disks"` // Rutas de discos creados
}

var (
    diskRegistry     DiskRegistry
    diskRegistryMux  sync.RWMutex
    registryFilePath = filepath.Join(os.TempDir(), "extreamfs_disk_registry.json")
)

// Inicializar el registro al inicio
func init() {
    loadDiskRegistry()
}

// Cargar el registro desde archivo
func loadDiskRegistry() {
    diskRegistryMux.Lock()
    defer diskRegistryMux.Unlock()

    data, err := os.ReadFile(registryFilePath)
    if err != nil {
        // Si no existe, inicializar vacío
        diskRegistry = DiskRegistry{Disks: []string{}}
        return
    }

    if err := json.Unmarshal(data, &diskRegistry); err != nil {
        diskRegistry = DiskRegistry{Disks: []string{}}
    }

    // Validar que los discos existan
    validDisks := []string{}
    for _, diskPath := range diskRegistry.Disks {
        if _, err := os.Stat(diskPath); err == nil {
            validDisks = append(validDisks, diskPath)
        }
    }
    diskRegistry.Disks = validDisks
    saveDiskRegistryUnsafe() // Guardar solo discos válidos
}

// Guardar el registro a archivo (sin lock, para usar dentro de funciones con lock)
func saveDiskRegistryUnsafe() error {
    data, err := json.MarshalIndent(diskRegistry, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(registryFilePath, data, 0644)
}

// Agregar un disco al registro
func AddDiskToRegistry(diskPath string) error {
    diskRegistryMux.Lock()
    defer diskRegistryMux.Unlock()

    // Verificar si ya existe
    for _, path := range diskRegistry.Disks {
        if path == diskPath {
            return nil // Ya está registrado
        }
    }

    diskRegistry.Disks = append(diskRegistry.Disks, diskPath)
    return saveDiskRegistryUnsafe()
}

// Remover un disco del registro
func RemoveDiskFromRegistry(diskPath string) error {
    diskRegistryMux.Lock()
    defer diskRegistryMux.Unlock()

    newDisks := []string{}
    for _, path := range diskRegistry.Disks {
        if path != diskPath {
            newDisks = append(newDisks, path)
        }
    }

    diskRegistry.Disks = newDisks
    return saveDiskRegistryUnsafe()
}

// FUNCIÓN OPTIMIZADA: Obtener discos desde el registro
func GetAllDisksOptimized() []DiskInfo {
    diskRegistryMux.RLock()
    diskPaths := make([]string, len(diskRegistry.Disks))
    copy(diskPaths, diskRegistry.Disks)
    diskRegistryMux.RUnlock()

    var disks []DiskInfo

    for _, diskPath := range diskPaths {
        // Usar la función readDiskInfoOptimized que ya existe
        diskInfo := readDiskInfoOptimized(diskPath)
        if diskInfo != nil {
            disks = append(disks, *diskInfo)
        }
    }

    return disks
}

// Obtener solo la información de discos registrados (sin leer MBR)
func GetRegisteredDiskPaths() []string {
    diskRegistryMux.RLock()
    defer diskRegistryMux.RUnlock()

    paths := make([]string, len(diskRegistry.Disks))
    copy(paths, diskRegistry.Disks)
    return paths
}
