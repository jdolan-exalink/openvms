import type { LucideIcon } from "lucide-react";
import {
  AlertTriangle,
  Bike,
  Bird,
  Briefcase,
  Bus,
  Car,
  Cat,
  Dog,
  Footprints,
  Laptop,
  Lightbulb,
  Luggage,
  Package,
  PawPrint,
  Plane,
  Ship,
  ShoppingBag,
  Smartphone,
  Truck,
  Tv,
  Umbrella,
  User,
} from "lucide-react";

export type FrigateObjectCategory =
  | "people"
  | "vehicles"
  | "animals"
  | "accessories"
  | "safety"
  | "electronics";

export interface FrigateObjectDefinition {
  tag: string;
  label: string;
  category: FrigateObjectCategory;
  icon: LucideIcon;
  description?: string;
}

export const FRIGATE_OBJECT_CATEGORIES: { id: FrigateObjectCategory; label: string }[] = [
  { id: "people", label: "Personas" },
  { id: "vehicles", label: "Vehículos" },
  { id: "animals", label: "Animales" },
  { id: "accessories", label: "Accesorios & Paquetes" },
  { id: "safety", label: "Seguridad & Señalización" },
  { id: "electronics", label: "Dispositivos & Tecnología" },
];

export const FRIGATE_OBJECT_CATALOG: FrigateObjectDefinition[] = [
  // Personas
  { tag: "person", label: "Persona", category: "people", icon: User, description: "Detección de peatones y personas" },

  // Vehículos
  { tag: "car", label: "Automóvil / Auto", category: "vehicles", icon: Car, description: "Coches particulares y taxis" },
  { tag: "motorcycle", label: "Motocicleta", category: "vehicles", icon: Bike, description: "Motos y ciclomotores" },
  { tag: "bicycle", label: "Bicicleta", category: "vehicles", icon: Bike, description: "Bicicletas convencionales y eléctricas" },
  { tag: "bus", label: "Autobús / Colectivo", category: "vehicles", icon: Bus, description: "Transporte público y colectivos" },
  { tag: "truck", label: "Camión", category: "vehicles", icon: Truck, description: "Camiones de carga y reparto" },
  { tag: "boat", label: "Embarcación / Barco", category: "vehicles", icon: Ship, description: "Lanchas, barcos y botes" },
  { tag: "airplane", label: "Avión", category: "vehicles", icon: Plane, description: "Aeronaves en pista o vuelo" },

  // Animales
  { tag: "dog", label: "Perro", category: "animals", icon: Dog, description: "Mascotas y perros guardianes" },
  { tag: "cat", label: "Gato", category: "animals", icon: Cat, description: "Mascotas y felinos" },
  { tag: "bird", label: "Pájaro / Ave", category: "animals", icon: Bird, description: "Aves y pájaros en exteriores" },
  { tag: "horse", label: "Caballo", category: "animals", icon: Footprints, description: "Equinos en zonas rurales o predios" },
  { tag: "sheep", label: "Oveja", category: "animals", icon: PawPrint, description: "Ovinos y ganado menor" },
  { tag: "cow", label: "Vaca / Ganado", category: "animals", icon: PawPrint, description: "Bovinos y ganado mayor" },
  { tag: "bear", label: "Oso / Fauna silvestre", category: "animals", icon: PawPrint, description: "Animales silvestres grandes" },

  // Accesorios & Paquetes
  { tag: "package", label: "Paquete / Encomienda", category: "accessories", icon: Package, description: "Cajas de entrega y paquetería" },
  { tag: "backpack", label: "Mochila", category: "accessories", icon: Briefcase, description: "Mochilas de repartidores o transeúntes" },
  { tag: "handbag", label: "Bolso / Cartera", category: "accessories", icon: ShoppingBag, description: "Bolsos de mano y carteras" },
  { tag: "suitcase", label: "Valija / Equipaje", category: "accessories", icon: Luggage, description: "Maletas de viaje y valijas" },
  { tag: "umbrella", label: "Paraguas", category: "accessories", icon: Umbrella, description: "Paraguas abiertos o cerrados" },

  // Seguridad & Señalización
  { tag: "traffic light", label: "Semáforo", category: "safety", icon: Lightbulb, description: "Semáforos viales" },
  { tag: "stop sign", label: "Señal Pare / Stop", category: "safety", icon: AlertTriangle, description: "Carteles de detención obligatoria" },
  { tag: "fire hydrant", label: "Boca de incendio / Hidrante", category: "safety", icon: AlertTriangle, description: "Hidrantes de incendio" },

  // Dispositivos & Tecnología
  { tag: "laptop", label: "Notebook / Laptop", category: "electronics", icon: Laptop, description: "Computadoras portátiles" },
  { tag: "cell phone", label: "Teléfono celular", category: "electronics", icon: Smartphone, description: "Teléfonos móviles y smartphones" },
  { tag: "tv", label: "Monitor / Pantalla", category: "electronics", icon: Tv, description: "Televisores y pantallas de monitoreo" },
];

export const FRIGATE_SUPPORTED_OBJECTS = new Set<string>([
  ...FRIGATE_OBJECT_CATALOG.map((o) => o.tag),
  // Additional COCO 80 dataset labels supported by Frigate detector models
  "elephant",
  "zebra",
  "giraffe",
  "tie",
  "skis",
  "snowboard",
  "sports ball",
  "kite",
  "baseball bat",
  "baseball glove",
  "skateboard",
  "surfboard",
  "tennis racket",
  "bottle",
  "wine glass",
  "cup",
  "fork",
  "knife",
  "spoon",
  "bowl",
  "banana",
  "apple",
  "sandwich",
  "orange",
  "broccoli",
  "carrot",
  "hot dog",
  "pizza",
  "donut",
  "cake",
  "chair",
  "couch",
  "potted plant",
  "bed",
  "dining table",
  "toilet",
  "mouse",
  "remote",
  "keyboard",
  "microwave",
  "oven",
  "toaster",
  "sink",
  "refrigerator",
  "book",
  "clock",
  "vase",
  "scissors",
  "teddy bear",
  "hair drier",
  "toothbrush",
]);

export function isFrigateSupportedObject(tag: string): boolean {
  return FRIGATE_SUPPORTED_OBJECTS.has(tag.trim().toLowerCase());
}

/**
 * Parses tracked object tags from Frigate YAML configuration.
 */
export function parseTrackedObjects(yaml: string): string[] {
  const lines = yaml.split("\n");
  let inObjects = false;
  let inTrack = false;
  let trackIndent = -1;
  const tracked: string[] = [];

  for (const line of lines) {
    // Check top-level sections
    if (/^[a-zA-Z0-9_-]+:/.test(line)) {
      if (line.startsWith("objects:")) {
        inObjects = true;
        inTrack = false;
        continue;
      } else if (inObjects) {
        break; // Exited objects section
      }
    }

    if (inObjects) {
      const matchTrack = line.match(/^(\s*)track:\s*$/);
      if (matchTrack) {
        inTrack = true;
        trackIndent = (matchTrack[1] ?? "").length;
        continue;
      }

      if (inTrack) {
        const itemMatch = line.match(/^\s*-\s*["']?([a-zA-Z0-9_\s-]+?)["']?\s*$/);
        if (itemMatch && itemMatch[1]) {
          tracked.push(itemMatch[1].trim());
        } else if (line.trim().length > 0 && !line.trim().startsWith("#")) {
          // If indented same or less than track: key, we exited track
          const currentIndent = (line.match(/^(\s*)/)?.[1] ?? "").length;
          if (currentIndent <= trackIndent) {
            inTrack = false;
          }
        }
      }
    }
  }

  return tracked;
}

/**
 * Toggles a tracked object tag in Frigate YAML configuration.
 */
export function toggleTrackedObject(yaml: string, tag: string): string {
  const currentTracked = parseTrackedObjects(yaml);
  const exists = currentTracked.includes(tag);
  const nextTracked = exists
    ? currentTracked.filter((t) => t !== tag)
    : [...currentTracked, tag];

  const lines = yaml.split("\n");
  let inObjects = false;
  let objectsStart = -1;
  let objectsEnd = -1;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (/^[a-zA-Z0-9_-]+:/.test(line)) {
      if (line.startsWith("objects:")) {
        inObjects = true;
        objectsStart = i;
      } else if (inObjects) {
        objectsEnd = i;
        break;
      }
    }
  }

  if (inObjects && objectsEnd === -1) {
    objectsEnd = lines.length;
  }

  const trackLines = nextTracked.map((t) => `    - ${t}`).join("\n");
  const newObjectsBlock =
    nextTracked.length > 0
      ? `objects:\n  track:\n${trackLines}`
      : `objects:\n  track: []`;

  if (objectsStart !== -1) {
    const before = lines.slice(0, objectsStart).join("\n");
    const after = lines.slice(objectsEnd).join("\n");
    return [before, newObjectsBlock, after].filter(Boolean).join("\n");
  }

  // If no objects block existed, append it
  return `${yaml.trimEnd()}\n\n${newObjectsBlock}\n`;
}
