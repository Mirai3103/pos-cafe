import { motion, type Transition } from "motion/react"
import * as React from "react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface Ripple {
  id: number
  x: number
  y: number
}

export interface RippleButtonProps extends React.ComponentProps<typeof Button> {
  scale?: number
  transition?: Transition
}

export function RippleButton({
  children,
  className,
  scale = 10,
  transition = { duration: 0.6, ease: "easeOut" },
  onClick,
  ...props
}: RippleButtonProps) {
  const [ripples, setRipples] = React.useState<Ripple[]>([])

  const handleClick: React.ComponentProps<typeof Button>["onClick"] = (event) => {
    const rect = event.currentTarget.getBoundingClientRect()
    const id = Date.now()
    setRipples(prev => [...prev, { id, x: event.clientX - rect.left, y: event.clientY - rect.top }])
    setTimeout(() => {
      setRipples(prev => prev.filter(r => r.id !== id))
    }, 600)
    onClick?.(event)
  }

  return (
    <Button
      data-slot="ripple-button"
      onClick={handleClick}
      className={cn("relative overflow-hidden active:translate-y-0", className)}
      {...props}
    >
      {children}
      {ripples.map(ripple => (
        <motion.span
          aria-hidden
          key={ripple.id}
          initial={{ scale: 0, opacity: 0.5 }}
          animate={{ scale, opacity: 0 }}
          transition={transition}
          className="pointer-events-none absolute size-5 rounded-full bg-current"
          style={{ top: ripple.y - 10, left: ripple.x - 10 }}
        />
      ))}
    </Button>
  )
}

export default RippleButton
