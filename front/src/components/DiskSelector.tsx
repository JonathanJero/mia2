import React from 'react';
import '../App.css';

interface Partition {
  name: string;
  id: string;
  size: number;
  type: string;
  isMounted: boolean;
  status: string;
}

interface Disk {
  path: string;
  size: number;
  unit: string;
  fit: string;
  partitions: Partition[];
}

interface DiskSelectorProps {
  disks: Disk[];
  onSelectDisk: (disk: Disk) => void;
}

const DiskSelector: React.FC<DiskSelectorProps> = ({ disks, onSelectDisk }) => {
  const formatSize = (size: number, unit: string) => {
    return `${size} ${unit}`;
  };

  const getDiskIcon = (path: string) => {
    if (path.includes('A.mia')) return '💽 A.mia';
    if (path.includes('B.mia')) return '💿 B.mia';
    if (path.includes('C.mia')) return '📀 C.mia';
    if (path.includes('D.mia')) return '💾 D.mia';
    return '💽 Disco';
  };

  return (
    <div className="disk-selector">
      <div className="selector-header">
        <h3>Selecciona el disco que deseas visualizar</h3>
        <p className="selector-subtitle">
          {disks.length === 0 
            ? 'No hay discos disponibles' 
            : `${disks.length} disco${disks.length !== 1 ? 's' : ''} encontrado${disks.length !== 1 ? 's' : ''}`
          }
        </p>
      </div>

      {disks.length === 0 ? (
        <div className="no-disks">
          <div className="no-disks-icon">📁</div>
          <p>No se encontraron discos creados</p>
          <small>Usa el comando 'mkdisk' para crear un disco</small>
        </div>
      ) : (
        <div className="disks-grid">
          {disks.map((disk, index) => (
            <div 
              key={index}
              className="disk-card"
              onClick={() => onSelectDisk(disk)}
            >
              <div className="disk-card-header">
                <div className="disk-icon-large">
                  {getDiskIcon(disk.path).split(' ')[0]}
                </div>
                <div className="disk-name">
                  {getDiskIcon(disk.path).split(' ')[1]}
                </div>
              </div>

              <div className="disk-card-body">
                <div className="disk-info-row">
                  <span className="info-label">Capacidad:</span>
                  <span className="info-value">{formatSize(disk.size, disk.unit)}</span>
                </div>

                <div className="disk-info-row">
                  <span className="info-label">Ajuste:</span>
                  <span className="info-value">{disk.fit || 'N/A'}</span>
                </div>

                <div className="disk-info-row">
                  <span className="info-label">Particiones:</span>
                  <span className="info-value">
                    {disk.partitions?.length || 0}
                    {disk.partitions?.filter(p => p.isMounted).length > 0 && (
                      <span className="mounted-badge">
                        {disk.partitions.filter(p => p.isMounted).length} montada(s)
                      </span>
                    )}
                  </span>
                </div>

                {disk.partitions && disk.partitions.length > 0 && (
                  <div className="partitions-preview">
                    {disk.partitions.slice(0, 3).map((partition, pIndex) => (
                      <div key={pIndex} className="partition-tag">
                        <span className={`partition-status ${partition.isMounted ? 'mounted' : 'unmounted'}`}>
                          {partition.isMounted ? '🟢' : '⚪'}
                        </span>
                        {partition.name}
                      </div>
                    ))}
                    {disk.partitions.length > 3 && (
                      <div className="partition-tag more">
                        +{disk.partitions.length - 3} más
                      </div>
                    )}
                  </div>
                )}
              </div>

              <div className="disk-card-footer">
                <button className="btn-explore">
                  <span>👁️</span>
                  Explorar
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default DiskSelector;