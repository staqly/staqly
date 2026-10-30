'use client'

import {
  Briefcase,
  Building2,
  Camera,
  Code,
  Coffee,
  Flame,
  Folder,
  Gamepad2,
  Globe,
  GraduationCap,
  Heart,
  House,
  Inbox,
  Leaf,
  type LucideIcon,
  Megaphone,
  Music,
  Palette,
  Rocket,
  ShoppingBag,
  Sparkles,
  Star,
  Store,
  Trophy,
  User,
  Users,
  Zap,
  Car,
  Plane,
  Bike,
  Utensils,
  Pizza,
  Wine,
  Dumbbell,
  Book,
  Film,
  Tv,
  Mic,
  Headphones,
  Gift,
  Shirt,
  Gem,
  Crown,
  PawPrint,
  Flower2,
  TreePine,
  Mountain,
  Sun,
  Moon,
  Lightbulb,
  Target,
  Flag,
  Bell,
  Mail,
  Newspaper,
  Landmark,
  Banknote,
  Shield,
  Bot,
} from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import type { ChannelGroup } from '@/lib/api'
import { cn } from '@/lib/utils'

/** The icons a channel group can have, by the name the API stores (the same list as app.channelGroupIcons). */
const ICONS: Record<string, LucideIcon> = {
  briefcase: Briefcase,
  building: Building2,
  store: Store,
  house: House,
  user: User,
  users: Users,
  heart: Heart,
  star: Star,
  rocket: Rocket,
  sparkles: Sparkles,
  zap: Zap,
  flame: Flame,
  trophy: Trophy,
  globe: Globe,
  megaphone: Megaphone,
  camera: Camera,
  music: Music,
  gamepad: Gamepad2,
  coffee: Coffee,
  'shopping-bag': ShoppingBag,
  'graduation-cap': GraduationCap,
  palette: Palette,
  code: Code,
  leaf: Leaf,
  car: Car,
  plane: Plane,
  bike: Bike,
  utensils: Utensils,
  pizza: Pizza,
  wine: Wine,
  dumbbell: Dumbbell,
  book: Book,
  film: Film,
  tv: Tv,
  mic: Mic,
  headphones: Headphones,
  gift: Gift,
  shirt: Shirt,
  gem: Gem,
  crown: Crown,
  'paw-print': PawPrint,
  flower: Flower2,
  tree: TreePine,
  mountain: Mountain,
  sun: Sun,
  moon: Moon,
  lightbulb: Lightbulb,
  target: Target,
  flag: Flag,
  bell: Bell,
  mail: Mail,
  newspaper: Newspaper,
  landmark: Landmark,
  banknote: Banknote,
  shield: Shield,
  bot: Bot,
}

/** A group's icon; without one, the Default group gets an inbox and the others a folder. */
export function GroupIcon({ group, className }: { group: ChannelGroup; className?: string }) {
  // Without a picked icon: the Default group's inbox, any other group a folder.
  const Icon = ICONS[group.icon] ?? (group.default ? Inbox : Folder)

  return <Icon aria-hidden className={className} />
}

/** A square button showing the group's icon; it opens the set to pick one from, or None. */
export function GroupIconPicker({ value, onChange }: { value: string; onChange: (name: string) => void }) {
  const [open, setOpen] = useState(false)
  const Icon = ICONS[value] ?? Folder
  const pick = (name: string) => {
    onChange(name)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="shrink-0"
          aria-label={value ? `Icon: ${value}` : 'Choose an icon'}
          data-testid="group-icon"
        >
          <Icon className={cn(!ICONS[value] && 'text-muted-foreground')} />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-auto p-2">
        <div role="group" aria-label="Icons" className="grid grid-cols-8 gap-1">
          {Object.entries(ICONS).map(([name, I]) => (
            <Button
              key={name}
              type="button"
              variant={name === value ? 'secondary' : 'ghost'}
              size="icon"
              aria-label={name}
              aria-pressed={name === value}
              onClick={() => pick(name)}
            >
              <I />
            </Button>
          ))}
        </div>
        {value && (
          <Button type="button" variant="ghost" size="sm" className="mt-1 w-full" onClick={() => pick('')}>
            No icon
          </Button>
        )}
      </PopoverContent>
    </Popover>
  )
}
