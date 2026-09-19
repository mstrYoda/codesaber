import { iconNames, IconName } from "./IconNames";



interface IconProps {
    name: IconName
    iconProps?: React.SVGProps<SVGSVGElement>
}



function Icon({ name, iconProps }: IconProps) {

  const icon = iconNames.find((icon) => icon.name === name)

  return icon ? <icon.component {...iconProps} /> : null
  
}

export default Icon 